#ifdef _WIN32
#include <winsock2.h>
#include <ws2tcpip.h>
#else
#include <arpa/inet.h>
#include <sys/socket.h>
#include <sys/types.h>
#endif

#include <stdbool.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include <uv.h>
#include "udx.h"

#define HEARTBEAT_INTERVAL_MS 5000

typedef struct app_s {
  uv_loop_t *loop;
  udx_t udx;
  udx_socket_t socket;
  udx_stream_t stream;
  uv_timer_t heartbeat_timer;

  bool connected;
  bool connect_requested;

  uint32_t local_id;
  uint32_t peer_id;

  uint32_t bind_port;
  char node_name[256];

  char peer_host[256];
  uint32_t peer_port;
} app_t;

static void on_write_done(udx_stream_write_t *req, int status, int unordered) {
  (void)status;
  (void)unordered;
  free(req);
}

static void on_read(udx_stream_t *stream, ssize_t nread, const uv_buf_t *buf) {
  (void)stream;
  if (nread > 0) {
    printf("mesh: received data: %.*s\n", (int)nread, buf->base);
    fflush(stdout);
  }
}

static void send_heartbeat(app_t *app) {
  char msg[256];
  int len = snprintf(msg, sizeof(msg), "heartbeat-from-%s", app->node_name);
  uv_buf_t buf = uv_buf_init(msg, len);
  udx_stream_write_t *req = malloc(udx_stream_write_sizeof(1));
  if (req == NULL) return;
  udx_stream_write(req, &app->stream, &buf, 1, on_write_done);
  printf("mesh: sent heartbeat\n");
  fflush(stdout);
}

static void on_heartbeat_tick(uv_timer_t *timer) {
  app_t *app = timer->data;
  if (!app->connected) return;
  send_heartbeat(app);
}

static int parse_args(app_t *app, int argc, char **argv) {
  for (int i = 1; i < argc; i++) {
    if (strcmp(argv[i], "--name") == 0 && i + 1 < argc) {
      strncpy(app->node_name, argv[++i], sizeof(app->node_name) - 1);
    } else if (strcmp(argv[i], "--bind-port") == 0 && i + 1 < argc) {
      app->bind_port = (uint32_t)atoi(argv[++i]);
    } else if (strcmp(argv[i], "--peer-host") == 0 && i + 1 < argc) {
      strncpy(app->peer_host, argv[++i], sizeof(app->peer_host) - 1);
      app->connect_requested = true;
    } else if (strcmp(argv[i], "--peer-port") == 0 && i + 1 < argc) {
      app->peer_port = (uint32_t)atoi(argv[++i]);
      app->connect_requested = true;
    } else if (strcmp(argv[i], "--local-id") == 0 && i + 1 < argc) {
      app->local_id = (uint32_t)atoi(argv[++i]);
    } else if (strcmp(argv[i], "--peer-id") == 0 && i + 1 < argc) {
      app->peer_id = (uint32_t)atoi(argv[++i]);
    }
  }

  if (app->bind_port == 0) {
    fprintf(stderr, "mesh v2: --bind-port is required\n");
    return -1;
  }

  if (app->connect_requested) {
    if (app->peer_host[0] == '\0' || app->peer_port == 0) {
      fprintf(stderr, "mesh v2: when connecting, both --peer-host and --peer-port are required\n");
      return -1;
    }
  }

  return 0;
}

int main(int argc, char **argv) {
  app_t app;
  memset(&app, 0, sizeof(app));

  app.local_id = 1;
  app.peer_id = 2;
  strncpy(app.node_name, "unknown", sizeof(app.node_name) - 1);

  if (parse_args(&app, argc, argv) != 0) {
    return 1;
  }

  app.loop = uv_default_loop();

  udx_init(app.loop, &app.udx, NULL);
  udx_socket_init(&app.udx, &app.socket, NULL);

  struct sockaddr_in bind_addr;
  uv_ip4_addr("0.0.0.0", app.bind_port, &bind_addr);
  udx_socket_bind(&app.socket, (struct sockaddr *)&bind_addr, 0);

  udx_stream_init(&app.udx, &app.stream, app.local_id, NULL, NULL);
  udx_stream_read_start(&app.stream, on_read);

  if (app.connect_requested) {
    struct sockaddr_in peer_addr;
    if (uv_ip4_addr(app.peer_host, app.peer_port, &peer_addr) != 0) {
      fprintf(stderr, "mesh v2: invalid peer address %s:%u\n", app.peer_host, app.peer_port);
      return 1;
    }

    udx_stream_connect(&app.stream, &app.socket, app.peer_id, (struct sockaddr *)&peer_addr);
    app.connected = true;
    printf("mesh v2: %s connecting to %s:%u\n", app.node_name, app.peer_host, app.peer_port);
    fflush(stdout);
  } else {
    printf("mesh v2: %s listening on :%u (no active peer configured)\n", app.node_name, app.bind_port);
    fflush(stdout);
  }

  uv_timer_init(app.loop, &app.heartbeat_timer);
  app.heartbeat_timer.data = &app;
  uv_timer_start(&app.heartbeat_timer, on_heartbeat_tick, 1000, HEARTBEAT_INTERVAL_MS);

  return uv_run(app.loop, UV_RUN_DEFAULT);
}
