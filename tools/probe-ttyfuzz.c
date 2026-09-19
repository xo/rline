/* Decode byte streams with isocline's escape decoder, one case at a time, so
   that a fuzzer can compare the C answer against the Go one.

   This runs as a server rather than once per case, because starting a process
   for every input a fuzzer generates would make the whole thing too slow to
   be worth running.

   The channels need care. tty_readc_noblock asks FIONREAD about file
   descriptor 0 rather than about its own, so whatever is on file descriptor 0
   decides whether the decoder believes input is waiting. So the terminal is a
   pipe that is put on file descriptor 0, the bytes of each case are written
   into it, and the commands arrive on file descriptor 3 instead, which the
   caller passes in. Putting the commands on standard input would make the
   decoder think terminal input was waiting and then block reading a pipe that
   is empty.

   Protocol. One case per line on file descriptor 3:

     <utf8flag> <hex bytes, or - for none>

   One answer per case on standard output:

     <hex key code> <hex key code> ...

   with an empty line when the case decodes to nothing. Both escape waits are
   zero, so nothing ever waits and the answer depends only on the bytes. */
/* These must come before any system header. isocline.c sets them too, but by
   then the C library headers have already fixed which interfaces they expose,
   and completers.c needs lstat and the S_IF* constants. */
#define _XOPEN_SOURCE 700
#define _DEFAULT_SOURCE

#include "isocline.c"

#include <stdio.h>
#include <stdint.h>
#include <stdlib.h>
#include <unistd.h>
#include <fcntl.h>
#include <poll.h>

static alloc_t probe_mem = { &malloc, &realloc, &free };

/* The largest case accepted. A pipe holds far more than this, so writing a
   case can never block. */
#define MAXCASE 4096

/* The most keys read from one case, so that a stream of input cannot make the
   answer unbounded. */
#define MAXKEYS 4096

static tty_t fuzz_tty;
static int tty_read_fd = -1;
static int tty_write_fd = -1;

static int from_hex(int c) {
  if (c >= '0' && c <= '9') return c - '0';
  if (c >= 'a' && c <= 'f') return 10 + (c - 'a');
  if (c >= 'A' && c <= 'F') return 10 + (c - 'A');
  return -1;
}

/* Throw away anything a previous case left unread, so that cases cannot run
   into each other. */
static void drain_terminal(void) {
  for (;;) {
    struct pollfd p = { .fd = tty_read_fd, .events = POLLIN, .revents = 0 };
    if (poll(&p, 1, 0) != 1 || (p.revents & POLLIN) == 0) return;
    uint8_t buf[512];
    ssize_t n = read(tty_read_fd, buf, sizeof(buf));
    if (n <= 0) return;
  }
}

static void reset_tty(bool utf8) {
  memset(&fuzz_tty, 0, sizeof(fuzz_tty));
  fuzz_tty.mem = &probe_mem;
  fuzz_tty.fd_in = tty_read_fd;
  fuzz_tty.is_utf8 = utf8;
  fuzz_tty.esc_initial_timeout = 0;
  fuzz_tty.esc_timeout = 0;
}

/* Decode one case and print the keys it gives. */
static void run_case(bool utf8, const uint8_t* bytes, ssize_t n) {
  drain_terminal();
  reset_tty(utf8);
  if (n > 0 && write(tty_write_fd, bytes, (size_t)n) != (ssize_t)n) {
    /* Writing the case failed, which would make the answer meaningless, so
       say so rather than print a wrong one. */
    printf("!write-failed\n");
    fflush(stdout);
    return;
  }
  bool first = true;
  for (int i = 0; i < MAXKEYS; i++) {
    code_t code = 0;
    if (!tty_read_timeout(&fuzz_tty, 0, &code)) break;
    printf("%s%08x", first ? "" : " ", (unsigned)code);
    first = false;
  }
  printf("\n");
  fflush(stdout);
}

int main(void) {
  int fds[2];
  if (pipe(fds) != 0) { fprintf(stderr, "cannot create a pipe\n"); return 1; }
  tty_read_fd = fds[0];
  tty_write_fd = fds[1];
  /* tty_readc_noblock asks FIONREAD about file descriptor 0, so the terminal
     has to be there as well as on its own descriptor. */
  if (dup2(tty_read_fd, 0) < 0) { fprintf(stderr, "cannot replace stdin\n"); return 1; }

  FILE* commands = fdopen(3, "r");
  if (commands == NULL) { fprintf(stderr, "no command channel on fd 3\n"); return 1; }

  char* line = NULL;
  size_t cap = 0;
  static uint8_t bytes[MAXCASE];
  while (getline(&line, &cap, commands) > 0) {
    /* <utf8flag> <hex> */
    char* p = line;
    while (*p == ' ') p++;
    if (*p != '0' && *p != '1') continue;
    const bool utf8 = (*p == '1');
    p++;
    while (*p == ' ') p++;

    ssize_t n = 0;
    if (*p == '-') {
      n = 0;
    }
    else {
      while (n < MAXCASE) {
        const int hi = from_hex((unsigned char)p[0]);
        if (hi < 0) break;
        const int lo = from_hex((unsigned char)p[1]);
        if (lo < 0) break;
        bytes[n++] = (uint8_t)(hi * 16 + lo);
        p += 2;
      }
    }
    run_case(utf8, bytes, n);
  }
  free(line);
  return 0;
}
