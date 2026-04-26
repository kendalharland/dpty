// Author: kendal@thebrowser.company
// Copyright (c) 2026 kendal@thebrowser.company. All rights reserved.

#define _XOPEN_SOURCE 600
#define _DEFAULT_SOURCE

#include <stdio.h>
#include <stdlib.h>
#include <unistd.h>

// pty.h is not part of the standard apple header set.
#ifdef __APPLE__
#include <util.h>
#else
#include <pty.h>
#endif

#include <termios.h>
#include <sys/types.h>
#include <sys/socket.h>
#include <sys/select.h>
#include <sys/wait.h>
#include <sys/ioctl.h>
#include <netinet/in.h>
#include <arpa/inet.h>
#include <string.h>
#include <errno.h>
#include <signal.h>

#define LISTEN_PORT 5137
#define BUF_SIZE 4096

static void die(const char *msg) {
    perror(msg);
    exit(EXIT_FAILURE);
}

static int create_server_socket(int port) {
    int sockfd;
    int opt = 1;
    struct sockaddr_in addr;

    sockfd = socket(AF_INET, SOCK_STREAM, 0);
    if (sockfd < 0) die("socket");

    if (setsockopt(sockfd, SOL_SOCKET, SO_REUSEADDR, &opt, sizeof(opt)) < 0) {
        die("setsockopt");
    }

    memset(&addr, 0, sizeof(addr));
    addr.sin_family = AF_INET;
    addr.sin_port   = htons(port);
    addr.sin_addr.s_addr = htonl(INADDR_ANY);

    if (bind(sockfd, (struct sockaddr *)&addr, sizeof(addr)) < 0) {
        die("bind");
    }

    if (listen(sockfd, 1) < 0) {
        die("listen");
    }

    return sockfd;
}

extern char **environ;

static char **merge_env_with_extra(char *const extra[]) {
    // Count existing environ entries
    size_t base_count = 0;
    while (environ[base_count] != NULL) {
        base_count++;
    }

    // Count extra entries
    size_t extra_count = 0;
    while (extra[extra_count] != NULL) {
        extra_count++;
    }

    // Allocate new envp: base + extra + NULL
    char **new_envp = malloc((base_count + extra_count + 1) * sizeof(char *));
    if (!new_envp) {
        return NULL;
    }

    // Copy existing pointers (we’re not duplicating strings; they stay owned by environ)
    for (size_t i = 0; i < base_count; i++) {
        new_envp[i] = environ[i];
    }

    // Append extra variables (these should be static / long-lived strings)
    for (size_t i = 0; i < extra_count; i++) {
        new_envp[base_count + i] = extra[i];
    }

    new_envp[base_count + extra_count] = NULL;
    return new_envp;
}

static pid_t spawn_shell_in_pty(int *master_fd) {
    struct winsize ws;
    int mfd, sfd;
    pid_t pid;

    memset(&ws, 0, sizeof(ws));
    ws.ws_col = 100;
    ws.ws_row = 35;

    /* Let openpty derive a normal termios from the current environment */
    if (openpty(&mfd, &sfd, NULL, NULL, &ws) < 0) {
        die("openpty");
    }

    pid = fork();
    if (pid < 0) {
        die("fork");
    }

    if (pid == 0) {
        /* Child: make the slave the controlling terminal and exec shell */
        close(mfd);

        if (setsid() < 0) {
            perror("setsid");
            _exit(1);
        }

        if (ioctl(sfd, TIOCSCTTY, 0) < 0) {
            perror("ioctl TIOCSCTTY");
        }

        dup2(sfd, STDIN_FILENO);
        dup2(sfd, STDOUT_FILENO);
        dup2(sfd, STDERR_FILENO);
        if (sfd > STDERR_FILENO) close(sfd);

        char *shell = "/bin/bash";
        char *agent = "claude";
        char *const argv[] = { shell, "-l", "-c", agent, "chat", NULL };
        char *const extra_env[] = {
            "MESSAGE=\"This is a message from the server\"",
            NULL,
        };

        char **envp = merge_env_with_extra(extra_env);
        if (!envp) {
            perror("malloc");
            _exit(1);
        }

        execve(shell, argv, envp);
        perror("execve");
        _exit(1);
    }

    /* Parent: just return the PTY master; no prompt injection here */
    close(sfd);
    *master_fd = mfd;
    return pid;
}



static void handle_client(int client_fd) {
    int pty_fd;
    pid_t child_pid = spawn_shell_in_pty(&pty_fd);
    fprintf(stderr, "Spawned shell PID %d\n", (int)child_pid);

    char buf[BUF_SIZE];
    int maxfd = (pty_fd > client_fd ? pty_fd : client_fd) + 1;
    int running = 1;

    while (running) {
        fd_set rfds;
        FD_ZERO(&rfds);
        FD_SET(client_fd, &rfds);
        FD_SET(pty_fd, &rfds);

        int ret = select(maxfd, &rfds, NULL, NULL, NULL);
        if (ret < 0) {
            if (errno == EINTR) continue;
            perror("select");
            break;
        }

        // From client -> PTY
        if (FD_ISSET(client_fd, &rfds)) {
            ssize_t n = read(client_fd, buf, sizeof(buf));
            if (n <= 0) {
                running = 0;
            } else {
                ssize_t off = 0;
                while (off < n) {
                    ssize_t w = write(pty_fd, buf + off, (size_t)(n - off));
                    if (w <= 0) {
                        running = 0;
                        break;
                    }
                    off += w;
                }
            }
        }

        // From PTY -> client
        if (FD_ISSET(pty_fd, &rfds)) {
            ssize_t n = read(pty_fd, buf, sizeof(buf));
            if (n <= 0) {
                running = 0;
            } else {
                ssize_t off = 0;
                while (off < n) {
                    ssize_t w = write(client_fd, buf + off, (size_t)(n - off));
                    if (w <= 0) {
                        running = 0;
                        break;
                    }
                    off += w;
                }
            }
        }
    }

    close(client_fd);
    close(pty_fd);

    // Reap child
    int status;
    waitpid(child_pid, &status, 0);
    fprintf(stderr, "Shell PID %d exited\n", (int)child_pid);
}

void accept_connection(int server_fd) {
   struct sockaddr_in caddr;
   socklen_t clen = sizeof(caddr);
   int client_fd = accept(server_fd, (struct sockaddr *)&caddr, &clen);
   if (client_fd < 0) {
       perror("accept");
       return;
   }

   fprintf(stderr, "Client connected from %s:%d\n",
           inet_ntoa(caddr.sin_addr), ntohs(caddr.sin_port));

   int pid = fork();
   if (pid == 0) {
       // child
       // Simple: handle one client at a time in foreground
       close(server_fd);
       handle_client(client_fd);
       _exit(0);
   } else {
       // parent
       close(client_fd);
   }
}

int main(void) {
    signal(SIGPIPE, SIG_IGN); // don't die on broken pipe

    int server_fd = create_server_socket(LISTEN_PORT);
    fprintf(stderr, "PTY server listening on port %d\n", LISTEN_PORT);

    for (;;) {
        accept_connection(server_fd);
    }

    close(server_fd);
    return 0;
}

