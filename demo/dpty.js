// dpty.js — Browser client for the dpty broker, server, and attach protocol.
//
// Quick start:
//
//   import { Client, Attachment, attachWebSocketUrl } from './dpty.js';
//
//   const c = new Client('http://localhost:5127');
//   const target = await c.pickAvailableServer();
//   const { alias } = await c.createPTY(target.address, {
//     shell: 'claude', args: ['hello'],
//   });
//
//   const att = new Attachment(target.address, alias, {
//     onOutput: (text) => term.write(text),
//     onClose:  ()    => console.log('closed'),
//   });
//   att.resize(80, 24);
//   att.send('ls\r');
//
// Wire protocol used by /<alias>:
//   - BINARY frames carry raw PTY bytes both directions.
//   - TEXT frames are JSON control messages from the client. Currently
//     only {"type":"resize","cols":N,"rows":N}.

export const STATUS_AVAILABLE = 'AVAILABLE';
export const STATUS_UNAVAILABLE = 'UNAVAILABLE';
export const MAX_SESSION_NAME_LEN = 64;

const SESSION_NAME_RE = /^[A-Za-z0-9._-]{1,64}$/;

/** Returns true if s is a syntactically valid session alias. */
export function isValidSessionName(s) {
  return typeof s === 'string' && SESSION_NAME_RE.test(s);
}

// ---- Errors ----

export class DptyError extends Error {
  constructor(message) {
    super(message);
    this.name = 'DptyError';
  }
}
export class SessionExistsError extends DptyError {
  constructor(message) {
    super(message);
    this.name = 'SessionExistsError';
  }
}
export class InvalidNameError extends DptyError {
  constructor(message) {
    super(message);
    this.name = 'InvalidNameError';
  }
}
export class NoServersError extends DptyError {
  constructor(message) {
    super(message);
    this.name = 'NoServersError';
  }
}

// ---- URL helpers ----

/**
 * Build the WebSocket URL a client should open to attach to alias on the
 * server at serverAddress. Pure: does no network I/O.
 */
export function attachWebSocketUrl(serverAddress, alias) {
  const host = String(serverAddress)
    .replace(/\/$/, '')
    .replace(/^https:/i, 'wss:')
    .replace(/^http:/i, 'ws:');
  return host + '/' + encodeURIComponent(alias);
}

// ---- Client ----

/**
 * Client talks to the dpty Broker and to individual Servers via HTTP.
 *
 * It is a thin wrapper around fetch(), so callers can swap it out or mock
 * it freely. All methods return Promises and throw DptyError subclasses
 * on protocol-level failures.
 */
export class Client {
  /** @param {string} brokerUrl e.g. "http://localhost:5127" */
  constructor(brokerUrl) {
    this.brokerUrl = String(brokerUrl).replace(/\/$/, '');
  }

  /** GET /servers on the broker. */
  async listServers() {
    return this._getJSON(this.brokerUrl + '/servers');
  }

  /** GET /sessions on the broker (aggregated across all AVAILABLE servers). */
  async listSessions() {
    return this._getJSON(this.brokerUrl + '/sessions');
  }

  /**
   * Choose the AVAILABLE server with the lowest load.
   * @throws {NoServersError} if no servers are AVAILABLE.
   */
  async pickAvailableServer() {
    const servers = await this.listServers();
    let best = null;
    for (const s of servers) {
      if (s.status !== STATUS_AVAILABLE) continue;
      if (!best || (s.load || 0) < (best.load || 0)) best = s;
    }
    if (!best) throw new NoServersError('no AVAILABLE dpty servers');
    return best;
  }

  /**
   * POST /pty on the chosen server.
   * @param {string} serverAddress base URL of the server (e.g. its `address` field)
   * @param {{shell:string, args?:string[], env?:string[], name?:string}} opts
   * @returns {Promise<{alias: string}>}
   * @throws {SessionExistsError} on a 409 (name collision).
   * @throws {InvalidNameError}   on a 400 (bad name characters).
   */
  async createPTY(serverAddress, opts) {
    const url = String(serverAddress).replace(/\/$/, '') + '/pty';
    const body = {
      shell: opts.shell,
      args: opts.args || [],
      env: opts.env || [],
    };
    if (opts.name) body.name = opts.name;

    const r = await fetch(url, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });
    if (r.status === 409) {
      throw new SessionExistsError(`session "${opts.name || ''}" already exists`);
    }
    if (r.status === 400) {
      throw new InvalidNameError(`invalid session name "${opts.name || ''}"`);
    }
    if (!r.ok) {
      throw new DptyError(`POST ${url} returned ${r.status}`);
    }
    return r.json();
  }

  async _getJSON(url) {
    const r = await fetch(url);
    if (!r.ok) throw new DptyError(`GET ${url} returned ${r.status}`);
    return r.json();
  }
}

// ---- Attachment ----

/**
 * Attachment wraps a WebSocket attached to one PTY session, exposing the
 * dpty wire protocol as small methods.
 *
 * Construction opens the WebSocket immediately. Pass handlers for
 * onOutput / onOpen / onClose / onError to react to its lifecycle. Output
 * is delivered as decoded UTF-8 strings; pass `binaryOutput: true` to get
 * raw Uint8Array chunks instead.
 */
export class Attachment {
  /**
   * @param {string} serverAddress
   * @param {string} alias
   * @param {{
   *   onOutput?: (chunk: string|Uint8Array) => void,
   *   onOpen?:   () => void,
   *   onClose?:  () => void,
   *   onError?:  (err: Event) => void,
   *   binaryOutput?: boolean,
   * }} [handlers]
   */
  constructor(serverAddress, alias, handlers = {}) {
    this.serverAddress = serverAddress;
    this.alias = alias;

    this._enc = new TextEncoder();
    this._dec = handlers.binaryOutput ? null : new TextDecoder('utf-8');

    this._onOutput = handlers.onOutput || (() => {});
    this._onOpen = handlers.onOpen || (() => {});
    this._onClose = handlers.onClose || (() => {});
    this._onError = handlers.onError || (() => {});

    const ws = new WebSocket(attachWebSocketUrl(serverAddress, alias));
    ws.binaryType = 'arraybuffer';
    this._ws = ws;

    ws.onopen = () => this._onOpen();
    ws.onclose = () => this._onClose();
    ws.onerror = (err) => this._onError(err);
    ws.onmessage = (ev) => this._handleMessage(ev);
  }

  /** Whether the underlying WebSocket is currently open. */
  get isOpen() {
    return this._ws.readyState === WebSocket.OPEN;
  }

  /**
   * Send raw input to the PTY as a BINARY frame. Strings are UTF-8
   * encoded. Uint8Array / ArrayBuffer are sent as-is. No-ops while the
   * connection is not yet open.
   */
  send(data) {
    if (!this.isOpen) return;
    if (typeof data === 'string') {
      this._ws.send(this._enc.encode(data));
    } else if (data instanceof Uint8Array) {
      this._ws.send(data);
    } else if (data instanceof ArrayBuffer) {
      this._ws.send(new Uint8Array(data));
    } else {
      throw new TypeError('Attachment.send: expected string, Uint8Array, or ArrayBuffer');
    }
  }

  /** Send a {"type":"resize",cols,rows} control message as a TEXT frame. */
  resize(cols, rows) {
    if (!this.isOpen) return;
    this._ws.send(JSON.stringify({ type: 'resize', cols, rows }));
  }

  /** Close the underlying WebSocket. */
  close() {
    this._ws.close();
  }

  _handleMessage(ev) {
    if (ev.data instanceof ArrayBuffer) {
      const bytes = new Uint8Array(ev.data);
      this._onOutput(this._dec ? this._dec.decode(bytes, { stream: true }) : bytes);
    } else {
      this._onOutput(ev.data);
    }
  }
}
