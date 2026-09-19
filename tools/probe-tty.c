/* Print the key codes that isocline's tty.c and tty_esc.c decode from a fixed
   set of byte sequences, so that the Go port can be checked against them.

   This file includes isocline.c, which is the unity build of the whole
   library. The functions of both modules are static there, so including the
   source is the only way to reach them.

   tty_new refuses a file descriptor that is not a terminal, so the probe
   builds a tty_t by hand instead. Every byte of a case is loaded into the
   low level pushback buffer, which tty_readc_noblock reads before it touches
   the file descriptor. Both escape timeouts are zero, so nothing waits and
   the output is the same on every run.

   The file descriptor is the read end of an empty pipe whose write end stays
   open, which stands in for a terminal that no one is typing on. It must not
   be /dev/null. A terminal with nothing pending is not readable, so the read
   never runs and tty_readc_noblock leaves the byte alone, which is what its
   comment promises. /dev/null is always readable, so the read does run,
   tty_readc_blocking clears the byte to zero first, and the decoder then sees
   a zero where it should see the byte it peeked at. ESC [ with nothing after
   it decodes to alt+'[' on a terminal and to alt+NUL through /dev/null.

   tty_readc_noblock also asks FIONREAD about file descriptor 0 rather than
   about its own, so the probe puts the same pipe on file descriptor 0.

   The output is a golden file. Every line is one case and the key codes it
   decoded to. */
/* These must come before any system header. isocline.c sets them too, but by
   then the C library headers have already fixed which interfaces they expose,
   and completers.c needs lstat and the S_IF* constants. */
#define _XOPEN_SOURCE 700
#define _DEFAULT_SOURCE

#include "isocline.c"

#include <stdio.h>
#include <stdint.h>
#include <stdlib.h>
#include <fcntl.h>
#include <unistd.h>

static alloc_t probe_mem = { &malloc, &realloc, &free };

/* The largest case the pushback buffer holds. */
#define MAXCASE TTY_PUSH_MAX

static tty_t probe_tty;
static int idle_fd = -1;

static void print_bytes(const uint8_t* b, int n) {
  if (n <= 0) { printf("-"); return; }
  for (int i = 0; i < n; i++) printf("%02x", b[i]);
}

/* Load one case. tty_cpop takes from the end of the buffer, so the bytes go
   in backwards and byte 0 comes out first. */
static void tty_reset(const uint8_t* bytes, int n, bool utf8) {
  memset(&probe_tty, 0, sizeof(probe_tty));
  probe_tty.mem = &probe_mem;
  probe_tty.fd_in = idle_fd;
  probe_tty.is_utf8 = utf8;
  probe_tty.esc_initial_timeout = 0;
  probe_tty.esc_timeout = 0;
  for (int i = 0; i < n; i++) {
    probe_tty.cpushbuf[i] = bytes[n - i - 1];
  }
  probe_tty.cpush_count = n;
}

/* Read every key the case decodes to. A case can produce more than one,
   because a sequence the decoder gives up on is pushed back and read again. */
static void key_case(const uint8_t* bytes, int n, bool utf8) {
  if (n > MAXCASE) return;
  tty_reset(bytes, n, utf8);
  printf("keys ");
  print_bytes(bytes, n);
  printf(" %d", utf8 ? 1 : 0);
  for (int i = 0; i < 16; i++) {
    code_t code = 0;
    if (!tty_read_timeout(&probe_tty, 0, &code)) break;
    printf(" %08x", (unsigned)code);
  }
  printf("\n");
}

/* Run a case in both input modes, because a byte above 0x7f decodes one way
   as UTF-8 and another way as a raw byte. */
static void key_case_both(const uint8_t* bytes, int n) {
  key_case(bytes, n, true);
  key_case(bytes, n, false);
}

static void seq(const char* s) {
  key_case_both((const uint8_t*)s, (int)strlen(s));
}

/* The decode tables on their own, so that a table entry that no sequence in
   the corpus reaches is still pinned. */
static void table_cases(void) {
  for (uint32_t v = 0; v <= 40; v++) {
    printf("vt %u %08x\n", v, (unsigned)esc_decode_vt(v));
  }
  for (int c = 0; c < 256; c++) {
    printf("xterm %02x %08x\n", c, (unsigned)esc_decode_xterm((uint8_t)c));
    printf("ss3 %02x %08x\n", c, (unsigned)esc_decode_ss3((uint8_t)c));
  }
}

/* The key code predicates and the normalizer, over the whole code space that
   matters: every byte, every virtual key, and each with every modifier. */
static void code_cases(void) {
  static const code_t bases[] = {
    0, 1, 7, 8, 9, 10, 13, 26, 27, 31, 32, 'a', 'A', '<', '>', 0x7E, 0x7F,
    0x80, 0xFF, 0x100, 0x10FFFF, 0x110000,
    KEY_UP, KEY_DOWN, KEY_LEFT, KEY_RIGHT, KEY_HOME, KEY_END, KEY_DEL,
    KEY_PAGEUP, KEY_PAGEDOWN, KEY_INS, KEY_F1, KEY_F12,
    KEY_EVENT_RESIZE, KEY_EVENT_AUTOTAB, KEY_EVENT_STOP
  };
  static const code_t mods[] = {
    0, KEY_MOD_SHIFT, KEY_MOD_ALT, KEY_MOD_CTRL,
    KEY_MOD_SHIFT | KEY_MOD_ALT, KEY_MOD_SHIFT | KEY_MOD_CTRL,
    KEY_MOD_ALT | KEY_MOD_CTRL,
    KEY_MOD_SHIFT | KEY_MOD_ALT | KEY_MOD_CTRL
  };
  for (int i = 0; i < (int)(sizeof(bases)/sizeof(bases[0])); i++) {
    for (int j = 0; j < (int)(sizeof(mods)/sizeof(mods[0])); j++) {
      code_t c = bases[i] | mods[j];
      char chr = 0;
      unicode_t u = 0;
      bool is_ascii = code_is_ascii_char(c, &chr);
      bool is_uni = code_is_unicode(c, &u);
      bool is_virt = code_is_virt_key(c);
      printf("code %08x %d %02x %d %08x %d %08x\n", (unsigned)c,
        is_ascii ? 1 : 0, (unsigned char)chr,
        is_uni ? 1 : 0, (unsigned)u,
        is_virt ? 1 : 0, (unsigned)modify_code(c));
    }
  }
}


/* tty_read_esc_response reads back the answer to a query that term.c wrote.
   The answer is recorded along with whatever was left unread, because the
   function pushes bytes back when it looks one too far ahead. */
static void esc_response_cases(void) {
  static const struct { const char* in; char start; int final_st; int buflen; } cases[] = {
    { "\x1B[24;80R", '[', 0, 64 },
    { "\x1B[?1;2c", '[', 0, 64 },
    { "\x1B[0n", '[', 0, 64 },
    { "\x1B[24;80R rest", '[', 0, 64 },
    { "\x1B[24;80R", '[', 0, 4 },
    { "\x1B[1234567890;1234567890R", '[', 0, 8 },
    { "\x1B]4;0;rgb:1111/2222/3333\x07", ']', 1, 64 },
    { "\x1B]4;0;rgb:11/22/33\x1B\\", ']', 1, 64 },
    { "\x1B]11;?\x02", ']', 1, 64 },
    { "\x1B]0;a\x1BZb\x07", ']', 1, 64 },
    { "\x1B]0;unterminated", ']', 1, 64 },
    { "\x1B]0;abc\x07tail", ']', 1, 64 },
    { "\x1BX24;80R", '[', 0, 64 },
    { "abc", '[', 0, 64 },
    { "\x1B", '[', 0, 64 },
    { "\x1B[", '[', 0, 64 },
    { "\x1B[24;80", '[', 0, 64 },
    { "\x1B]", ']', 1, 64 },
  };
  for (int i = 0; i < (int)(sizeof(cases)/sizeof(cases[0])); i++) {
    const uint8_t* in = (const uint8_t*)cases[i].in;
    const int n = (int)strlen(cases[i].in);
    if (n > MAXCASE) continue;
    char buf[128+1];
    memset(buf, 0, sizeof(buf));
    tty_reset(in, n, true);
    bool ok = tty_read_esc_response(&probe_tty, cases[i].start,
                                    cases[i].final_st != 0, buf, cases[i].buflen);
    printf("escresp ");
    print_bytes(in, n);
    printf(" %02x %d %d %d ", (unsigned char)cases[i].start, cases[i].final_st,
           cases[i].buflen, ok ? 1 : 0);
    print_bytes((const uint8_t*)buf, (int)strlen(buf));
    /* Whatever is left, so that pushing a byte back is recorded too. */
    printf(" ");
    uint8_t rest[MAXCASE + 1];
    int rn = 0;
    while (rn < MAXCASE) {
      uint8_t c;
      if (!tty_readc_noblock(&probe_tty, &c, 0)) break;
      rest[rn++] = c;
    }
    print_bytes(rest, rn);
    printf("\n");
  }
}

int main(void) {
  /* An empty pipe whose write end stays open: never readable, never at end
     of file, which is how a terminal looks when no one is typing. */
  int fds[2];
  if (pipe(fds) != 0) { fprintf(stderr, "cannot create a pipe\n"); return 1; }
  idle_fd = fds[0];
  /* tty_readc_noblock asks FIONREAD about file descriptor 0, so put the same
     idle pipe there. */
  if (dup2(idle_fd, 0) < 0) { fprintf(stderr, "cannot replace stdin\n"); return 1; }

  table_cases();
  code_cases();
  esc_response_cases();

  /* Every single byte, in both input modes. */
  for (int b = 0; b < 256; b++) {
    uint8_t one[1] = { (uint8_t)b };
    key_case_both(one, 1);
  }

  /* ESC followed by one byte: the Alt+char path, and the starts of every
     sequence the decoder knows. */
  for (int b = 0; b < 256; b++) {
    uint8_t two[2] = { 0x1B, (uint8_t)b };
    key_case_both(two, 2);
  }

  /* ESC ESC, which the decoder treats as an Alt modifier. */
  for (int b = 0; b < 256; b++) {
    uint8_t three[3] = { 0x1B, 0x1B, (uint8_t)b };
    key_case_both(three, 3);
  }

  /* CSI and SS3 with one final byte. */
  for (int start = 0; start < 5; start++) {
    const char* starts[5] = { "[", "O", "o", "?", "[[" };
    for (int f = 0x20; f < 0x80; f++) {
      uint8_t buf[8];
      int n = 0;
      buf[n++] = 0x1B;
      for (const char* p = starts[start]; *p != 0; p++) buf[n++] = (uint8_t)*p;
      buf[n++] = (uint8_t)f;
      key_case_both(buf, n);
    }
  }

  /* vt100 codes: ESC [ <num> ~ over every number the table names, and past
     the end of it. */
  for (int num = 0; num <= 40; num++) {
    char buf[16];
    snprintf(buf, sizeof(buf), "\x1B[%d~", num);
    seq(buf);
  }

  /* Modifiers as parameter 2, over every final the tables use. */
  static const char finals[] = "ABCDEFHIJLMNOPQRSTUVWXYZabcdruz~";
  for (int m = 0; m <= 10; m++) {
    for (const char* f = finals; *f != 0; f++) {
      char buf[24];
      snprintf(buf, sizeof(buf), "\x1B[1;%d%c", m, *f);
      seq(buf);
      snprintf(buf, sizeof(buf), "\x1BO1;%d%c", m, *f);
      seq(buf);
      /* Modifiers as parameter 1, which Haiku does. */
      snprintf(buf, sizeof(buf), "\x1B[%d%c", m, *f);
      seq(buf);
      snprintf(buf, sizeof(buf), "\x1BO%d%c", m, *f);
      seq(buf);
    }
  }

  /* vt100 codes with a modifier. */
  for (int num = 1; num <= 34; num++) {
    for (int m = 1; m <= 9; m++) {
      char buf[24];
      snprintf(buf, sizeof(buf), "\x1B[%d;%d~", num, m);
      seq(buf);
    }
  }

  /* Direct unicode: ESC [ <code> ; <mods> u */
  static const uint32_t unis[] = { 0, 1, 32, 65, 0x7F, 0x80, 0xFF, 0x3BB, 0x10FFFF, 0x110000 };
  for (int i = 0; i < (int)(sizeof(unis)/sizeof(unis[0])); i++) {
    for (int m = 1; m <= 8; m++) {
      char buf[32];
      snprintf(buf, sizeof(buf), "\x1B[%u;%du", unis[i], m);
      seq(buf);
    }
  }

  /* The special finals that the decoder rewrites. */
  static const char* specials[] = {
    "\x1B[@", "\x1B[9", "\x1B[1@", "\x1B[3^", "\x1B[2$", "\x1B[5@", "\x1B[15^",
    "\x1B[a", "\x1B[b", "\x1B[c", "\x1B[d", "\x1B[1;2a",
    "\x1B[:1~", "\x1B[<1~", "\x1B[=1~", "\x1B[>1~", "\x1B[?1~",
    "\x1B[?", "\x1B[:", "\x1BO?A", "\x1B[R", "\x1B[1;5R", "\x1B[99;99R",
    "\x1B[200~", "\x1B[201~", "\x1B[1;9A", "\x1B[1;10A",
    "\x1B[", "\x1BO", "\x1Bo", "\x1B[1", "\x1B[1;", "\x1B[1;2",
    "\x1B[12345678901234567890~",
  };
  for (int i = 0; i < (int)(sizeof(specials)/sizeof(specials[0])); i++) {
    seq(specials[i]);
  }

  /* OSC responses, which the decoder reads and throws away. */
  static const char* oscs[] = {
    "\x1B]0;title\x07", "\x1B]0;title\x1B\\", "\x1B]4;0;rgb:11/22/33\x07",
    "\x1B]0;title\x02", "\x1B]0;title", "\x1B]", "\x1B]\x07", "\x1B]\x1B\\",
    "\x1B]0;a\x1B""b\x07", "\x1B]0;title\x07x", "\x1B]0;title\x1B\\y",
  };
  for (int i = 0; i < (int)(sizeof(oscs)/sizeof(oscs[0])); i++) {
    seq(oscs[i]);
  }

  /* UTF-8 input, valid and not, in both modes. */
  static const char* utf8s[] = {
    "\xc3\xa9", "\xe6\x97\xa5", "\xf0\x9f\x98\x80", "\xc2\x80", "\xc2\xa0",
    "\xc3", "\xe6\x97", "\xf0\x9f\x98", "\xff", "\xfe\xff",
    "\xc3\xa9""a", "\x80", "\x80\x80", "\xed\xa0\x80", "\xc0\x80",
    "\x1B\xc3\xa9", "\x1B\xe6\x97\xa5",
  };
  for (int i = 0; i < (int)(sizeof(utf8s)/sizeof(utf8s[0])); i++) {
    seq(utf8s[i]);
  }

  /* Several keys in one buffer, so that leftover bytes are read again. */
  static const char* runs[] = {
    "abc", "\x1B[A\x1B[B", "a\x1B[Cb", "\x1B\x1B[A", "\x1B[Ax",
    "\r\n", "\x7F\x08", "\x1F", "\x1B[3~\x1B[3~",
  };
  for (int i = 0; i < (int)(sizeof(runs)/sizeof(runs[0])); i++) {
    seq(runs[i]);
  }

  return 0;
}
