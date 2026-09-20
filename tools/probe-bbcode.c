/* Print what isocline's bbcode.c produces, so that the Go port can be checked
   against it.

   bbcode_append needs no terminal, but bbcode_print does, so the probe opens a
   real pseudo-terminal the same way tools/probe-term.c does. */

#define _XOPEN_SOURCE 700
#define _DEFAULT_SOURCE

#if defined(__APPLE__)
/* cfmakeraw is a BSD extension, and asking for _XOPEN_SOURCE hides it on
   macOS. This asks for it back. It cannot affect Linux, where the macro does
   not exist and cfmakeraw is already visible through _DEFAULT_SOURCE. */
#define _DARWIN_C_SOURCE
#endif

#include "isocline.c"

#include <stdio.h>
#include <stdint.h>
#include <string.h>
#include <stdlib.h>
#include <fcntl.h>
#include <poll.h>

static alloc_t probe_mem = { &malloc, &realloc, &free };
static int leader_fd = -1;
static int follower_fd = -1;

static int open_pty(int cols, int rows) {
  leader_fd = posix_openpt(O_RDWR | O_NOCTTY);
  if (leader_fd < 0) return -1;
  if (grantpt(leader_fd) < 0 || unlockpt(leader_fd) < 0) return -1;
  char* name = ptsname(leader_fd);
  if (name == NULL) return -1;
  follower_fd = open(name, O_RDWR | O_NOCTTY);
  if (follower_fd < 0) return -1;
  struct termios tio;
  if (tcgetattr(follower_fd, &tio) == 0) { cfmakeraw(&tio); tcsetattr(follower_fd, TCSANOW, &tio); }
  struct winsize ws;
  memset(&ws, 0, sizeof(ws));
  ws.ws_col = (unsigned short)cols;
  ws.ws_row = (unsigned short)rows;
  if (ioctl(follower_fd, TIOCSWINSZ, &ws) < 0) return -1;
  return 0;
}

static void print_esc_bytes(const char* s, ssize_t n) {
  if (n <= 0) { printf("-"); return; }
  for (ssize_t i = 0; i < n; i++) {
    unsigned char c = (unsigned char)s[i];
    if (c == 0x1b) printf("\\e");
    else if (c == '\\') printf("\\\\");
    else if (c == '\r') printf("\\r");
    else if (c == '\n') printf("\\n");
    else if (c == '\t') printf("\\t");
    else if (c == ' ') printf("\\s");
    else if (c < 0x20 || c >= 0x7f) printf("\\x%02x", c);
    else printf("%c", c);
  }
}

static void emit(const char* tag) {
  char buf[8192];
  ssize_t total = 0;
  for (;;) {
    struct pollfd p; p.fd = leader_fd; p.events = POLLIN;
    if (poll(&p, 1, 20) <= 0) break;
    ssize_t n = read(leader_fd, buf + total, sizeof(buf) - (size_t)total);
    if (n <= 0) break;
    total += n;
    if (total >= (ssize_t)sizeof(buf)) break;
  }
  printf("out %s ", tag);
  print_esc_bytes(buf, total);
  printf("\n");
}

static void print_attr(attr_t a) {
  printf("%08x/%08x/%d/%d/%d/%d",
    (unsigned)a.x.color, (unsigned)a.x.bgcolor,
    (int)a.x.bold, (int)a.x.italic, (int)a.x.reverse, (int)a.x.underline);
}

/* Markup worth parsing. */
static const char* corpus[] = {
  "", "plain", "[b]bold[/b]", "[b]bold", "[/b]", "[]", "[ ]",
  "[b][i]both[/i][/b]", "[b][i]both[/b][/i]", "[b]a[/i]b[/b]",
  "[red]red[/red]", "[red]red[/]", "[#ff0000]hex[/]", "[#f00]short[/]",
  "[#ZZZZZZ]bad[/]", "[color=red]x[/]", "[color=#00ff00]x[/]",
  "[color=none]x[/]", "[color=nosuchcolor]x[/]",
  "[bgcolor=blue]x[/]", "[on blue]x[/]", "[on red]x[/]",
  "[ansi-red]x[/]", "[ansi-default]x[/]", "[ansi-color=33]x[/]",
  "[ansi-color=256]x[/]", "[ansi-color=999]x[/]", "[ansi-bgcolor=4]x[/]",
  "[ansi-sgr=1;31]x[/]", "[ansi-sgr=0]x[/]",
  "[bold=on]x[/]", "[bold=off]x[/]", "[bold=true]x[/]", "[bold=false]x[/]",
  "[bold=1]x[/]", "[bold=0]x[/]", "[bold=nonsense]x[/]", "[bold=]x[/]",
  "[italic=off]x[/]", "[underline=off]x[/]", "[reverse=off]x[/]",
  "[u]u[/u]", "[i]i[/i]", "[r]r[/r]", "[em]em[/em]", "[url]url[/url]",
  "[!pre]raw [b]not a tag[/b][/pre]", "[!pre]unterminated [b]x",
  "[!b]raw[/b]", "escaped \\[b] text", "backslash \\\\ here", "trailing \\\\",
  "esc \x1b[31m inside [b]bold[/b]",
  "[width=10]ab[/]", "[width=10;right]ab[/]", "[width=10;center]ab[/]",
  "[width=10;left;.]ab[/]", "[width=3]abcdefgh[/]",
  "[width=8;left;\\s;on]abcdefghij[/]", "[width=8;right;\\s;on]abcdefghij[/]",
  "[max-width=4]abcdefg[/]", "[max-width=4;right]abcdefg[/]",
  "[width=0]ab[/]", "[width=-3]ab[/]", "[width=notanumber]ab[/]",
  "[b][red]both[/red][/b]", "[b][red]both[/b][/red]",
  "[mystyle]custom[/mystyle]", "[other]second[/other]",
  "[B]upper[/B]", "[RED]upper[/RED]", "[color=RED]upper[/]",
  "[b   ]spaces[/b]", "[ b ]spaces[/ b ]",
  "[b]\xe6\x97\xa5\xe6\x9c\xac[/b]", "[width=6]\xe6\x97\xa5\xe6\x9c\xac[/]",
  "[color=\"red\"]quoted[/]", "[color=\"\"]empty[/]",
  "a[b]b[/b]c[i]d[/i]e"
};
#define NCORPUS ((int)(sizeof(corpus)/sizeof(corpus[0])))

/* Style names to resolve on their own. */
static const char* stylenames[] = {
  "b", "i", "u", "r", "em", "url", "red", "ansi-red", "#123456",
  "mystyle", "other", "nosuchthing", "", "bold", "color=red"
};

int main(void) {
  if (open_pty(80, 24) < 0) { fprintf(stderr, "no pty\n"); return 1; }
  setenv("TERM", "xterm-256color", 1);
  unsetenv("COLORTERM"); unsetenv("NO_COLOR");
  unsetenv("COLUMNS"); unsetenv("LINES");
  term_t* term = term_new(&probe_mem, NULL, false, true, follower_fd);
  term_set_buffer_mode(term, UNBUFFERED);
  bbcode_t* bb = bbcode_new(&probe_mem, term);
  emit("discard");

  /* Two user defined styles, so the style lookup has something to find. */
  bbcode_style_def(bb, "mystyle", "bold color=green");
  bbcode_style_def(bb, "other", "underline bgcolor=navy");

  for (int i = 0; i < (int)(sizeof(stylenames)/sizeof(stylenames[0])); i++) {
    printf("style "); print_esc_bytes(stylenames[i], (ssize_t)strlen(stylenames[i]));
    printf(" "); print_attr(bbcode_style(bb, stylenames[i])); printf("\n");
  }

  for (int i = 0; i < NCORPUS; i++) {
    stringbuf_t* out = sbuf_new(&probe_mem);
    attrbuf_t* ab = attrbuf_new(&probe_mem);
    bbcode_append(bb, corpus[i], out, ab);
    ssize_t n = sbuf_len(out);
    printf("append %d ", i);
    print_esc_bytes(sbuf_string(out), n);
    printf(" %d", (int)n);
    const attr_t* attrs = attrbuf_attrs(ab, n);
    for (ssize_t j = 0; j < n; j++) { printf(" "); print_attr(attrs[j]); }
    printf("\n");
    printf("width %d %d\n", i, (int)bbcode_column_width(bb, corpus[i]));
    sbuf_free(out);
    attrbuf_free(ab);
  }

  /* Printing, which drives the terminal. */
  for (int i = 0; i < NCORPUS; i++) {
    bbcode_print(bb, corpus[i]);
    term_flush(term);
    printf("print %d ", i);
    emit("");
  }
  bbcode_free(bb);
  term_free(term);
  return 0;
}
