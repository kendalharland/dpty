# Author: kendal@thebrowser.company
# Copyright (c) 2026 kendal@thebrowser.company. All rights reserved.

import asyncio
from pathlib import Path

from aiohttp import web
import aiohttp

PTY_HOST = "127.0.0.1"
PTY_PORT = 5137
HTTP_PORT = 8080

BASE_DIR = Path(__file__).resolve().parent
INDEX_PATH = BASE_DIR / "index.html"


async def index(request: web.Request) -> web.Response:
    try:
        html = INDEX_PATH.read_text(encoding="utf-8")
    except FileNotFoundError:
        return web.Response(
            text="index.html not found next to pty_web.py",
            status=500,
            content_type="text/plain",
        )
    return web.Response(text=html, content_type="text/html")


async def pty_ws(request: web.Request) -> web.WebSocketResponse:
    ws = web.WebSocketResponse()
    await ws.prepare(request)

    reader, writer = await asyncio.open_connection(PTY_HOST, PTY_PORT)

    async def pty_to_ws() -> None:
        try:
            while True:
                data = await reader.read(4096)
                if not data:
                    break
                await ws.send_bytes(data)
        except (asyncio.CancelledError, ConnectionResetError):
            pass
        finally:
            await ws.close()

    async def ws_to_pty() -> None:
        try:
            async for msg in ws:
                if msg.type == aiohttp.WSMsgType.TEXT:
                    writer.write(msg.data.encode("utf-8"))
                elif msg.type == aiohttp.WSMsgType.BINARY:
                    writer.write(msg.data)
                elif msg.type in (
                    aiohttp.WSMsgType.CLOSE,
                    aiohttp.WSMsgType.CLOSING,
                    aiohttp.WSMsgType.CLOSED,
                ):
                    break
                await writer.drain()
        except (asyncio.CancelledError, ConnectionResetError):
            pass
        finally:
            writer.close()
            try:
                await writer.wait_closed()
            except Exception:
                pass

    t1 = asyncio.create_task(pty_to_ws())
    t2 = asyncio.create_task(ws_to_pty())

    await asyncio.wait({t1, t2}, return_when=asyncio.FIRST_COMPLETED)
    for t in (t1, t2):
        if not t.done():
            t.cancel()

    return ws


def main() -> None:
    app = web.Application()
    app.router.add_get("/", index)
    app.router.add_get("/pty", pty_ws)
    web.run_app(app, port=HTTP_PORT)


if __name__ == "__main__":
    main()

