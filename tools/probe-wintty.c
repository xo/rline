/* Print the escape sequences that isocline pushes for a Windows key event,
   and the key codes they decode back to.

   Windows does not give a terminal a stream of bytes. It gives key events,
   and tty.c turns each one into the escape sequence that a Unix terminal
   would have sent, pushes that into its own buffer, and lets the same decoder
   read it back. So the Windows path is a producer of escape sequences and
   everything below it is shared.

   The three functions that do the producing sit above the part of tty.c that
   is compiled per system, so they build on any of them. That means the whole
   translation can be recorded here rather than on Windows, and only the
   console calls that fetch a key event are left needing a Windows host.

   The output is a golden file. Every line is one call and its result. */
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

static alloc_t probe_mem = { &malloc, &realloc, &free };

static tty_t probe_tty;
static int idle_fd = -1;

static void print_bytes(const uint8_t* b, ssize_t n) {
  if (n <= 0) { printf("-"); return; }
  for (ssize_t i = 0; i < n; i++) printf("%02x", b[i]);
}

static void reset_tty(void) {
  memset(&probe_tty, 0, sizeof(probe_tty));
  probe_tty.mem = &probe_mem;
  probe_tty.fd_in = idle_fd;
  probe_tty.is_utf8 = true;
  probe_tty.esc_initial_timeout = 0;
  probe_tty.esc_timeout = 0;
}

/* Print what is in the push back buffer, in the order the decoder will read
   it, which is the reverse of the order it is stored in. */
static void print_pushed(void) {
  uint8_t out[TTY_PUSH_MAX + 1];
  ssize_t n = 0;
  for (ssize_t i = probe_tty.cpush_count - 1; i >= 0; i--) {
    out[n++] = probe_tty.cpushbuf[i];
  }
  print_bytes(out, n);
}

/* Read back every key the pushed sequence decodes to, which is what the edit
   loop actually receives. */
static void print_decoded(void) {
  for (int i = 0; i < 8; i++) {
    code_t code = 0;
    if (!tty_read_timeout(&probe_tty, 0, &code)) break;
    printf(" %08x", (unsigned)code);
  }
}

/* Every combination of the three modifier keys. */
static const code_t modsets[] = {
  0,
  KEY_MOD_SHIFT,
  KEY_MOD_ALT,
  KEY_MOD_CTRL,
  KEY_MOD_SHIFT | KEY_MOD_ALT,
  KEY_MOD_SHIFT | KEY_MOD_CTRL,
  KEY_MOD_ALT | KEY_MOD_CTRL,
  KEY_MOD_SHIFT | KEY_MOD_ALT | KEY_MOD_CTRL,
};
#define NMODS ((int)(sizeof(modsets)/sizeof(modsets[0])))

static void vt_case(code_t mods, uint32_t vtcode) {
  reset_tty();
  tty_cpush_csi_vt(&probe_tty, mods, vtcode);
  printf("pushvt %08x %u ", (unsigned)mods, vtcode);
  print_pushed();
  print_decoded();
  printf("\n");
}

static void xterm_case(code_t mods, char xcode) {
  reset_tty();
  tty_cpush_csi_xterm(&probe_tty, mods, xcode);
  printf("pushxterm %08x %02x ", (unsigned)mods, (unsigned char)xcode);
  print_pushed();
  print_decoded();
  printf("\n");
}

static void unicode_case(code_t mods, uint32_t unicode) {
  reset_tty();
  tty_cpush_csi_unicode(&probe_tty, mods, unicode);
  printf("pushuni %08x %x ", (unsigned)mods, unicode);
  print_pushed();
  print_decoded();
  printf("\n");
}

int main(void) {
  int fds[2];
  if (pipe(fds) != 0) { fprintf(stderr, "cannot create a pipe\n"); return 1; }
  idle_fd = fds[0];
  if (dup2(idle_fd, 0) < 0) { fprintf(stderr, "cannot replace stdin\n"); return 1; }

  for (int m = 0; m < NMODS; m++) {
    printf("csimods %08x %u\n", (unsigned)modsets[m], csi_mods(modsets[m]));
  }

  /* The vt codes the Windows path sends: delete, page up, page down, and the
     function keys, plus the edges of the table. */
  static const uint32_t vtcodes[] = {
    0, 1, 2, 3, 5, 6, 10, 11, 12, 13, 14, 15, 17, 18, 19, 20, 21, 23, 24, 34, 99
  };
  for (int m = 0; m < NMODS; m++) {
    for (int i = 0; i < (int)(sizeof(vtcodes)/sizeof(vtcodes[0])); i++) {
      vt_case(modsets[m], vtcodes[i]);
    }
  }

  /* The xterm finals the Windows path sends for the cursor keys, plus the
     rest of the upper case range so that nothing in the table is missed. */
  for (int m = 0; m < NMODS; m++) {
    for (char c = 'A'; c <= 'Z'; c++) {
      xterm_case(modsets[m], c);
    }
  }

  /* Characters. The interesting part is which ones go through as a plain
     byte and which become a sequence. */
  static const uint32_t chars[] = {
    0, 1, 8, 9, 10, 13, 26, 27, 31, 32, 'a', 'A', '~', 0x7F, 0x80, 0xFF,
    0x100, 0x3BB, 0xFFFF, 0x10000, 0x10FFFF
  };
  for (int m = 0; m < NMODS; m++) {
    for (int i = 0; i < (int)(sizeof(chars)/sizeof(chars[0])); i++) {
      unicode_case(modsets[m], chars[i]);
    }
  }
  return 0;
}
