/* Print what isocline's term.c writes to a terminal, so that the Go port can
   be checked against it.

   The probe opens a real pseudo-terminal and hands the follower side to
   term_new as its output. A pipe would not do: term_update_dim falls back to
   asking the terminal for the cursor position when TIOCGWINSZ fails, and that
   path reads through the tty pointer, which is null here.

   The output is a golden file. The corpus is fixed, so the output does not
   change between runs. */

#define _XOPEN_SOURCE 700
#define _DEFAULT_SOURCE

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

/* Open a pseudo-terminal and fix its size, so that TIOCGWINSZ answers and the
   cursor query path is never taken. */
static int open_pty(int cols, int rows) {
  leader_fd = posix_openpt(O_RDWR | O_NOCTTY);
  if (leader_fd < 0) return -1;
  if (grantpt(leader_fd) < 0 || unlockpt(leader_fd) < 0) return -1;
  char* name = ptsname(leader_fd);
  if (name == NULL) return -1;
  follower_fd = open(name, O_RDWR | O_NOCTTY);
  if (follower_fd < 0) return -1;
  /* Raw mode, so the line discipline does not rewrite what isocline writes.
     In real use the tty puts the terminal in raw mode before any of this
     runs, so this matches rather than departs. */
  struct termios tio;
  if (tcgetattr(follower_fd, &tio) == 0) {
    cfmakeraw(&tio);
    tcsetattr(follower_fd, TCSANOW, &tio);
  }
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
    if (c == 0x1b) { printf("\\e"); }
    else if (c == '\\') { printf("\\\\"); }
    else if (c == '\r') { printf("\\r"); }
    else if (c == '\n') { printf("\\n"); }
    else if (c == '\t') { printf("\\t"); }
    else if (c < 0x20 || c >= 0x7f) { printf("\\x%02x", c); }
    else { printf("%c", c); }
  }
}

/* Flush the terminal and print everything it wrote since the last call. */
static void emit(const char* tag) {
  char buf[8192];
  ssize_t total = 0;
  for (;;) {
    struct pollfd p;
    p.fd = leader_fd;
    p.events = POLLIN;
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

static void flush_emit(term_t* term, const char* tag) {
  term_flush(term);
  emit(tag);
}

/* Build a term over the pseudo-terminal, with the environment already set. */
static term_t* new_term(void) {
  term_t* t = term_new(&probe_mem, NULL, false, true, follower_fd);
  return t;
}

static void print_attr(attr_t a) {
  printf("%08x %08x %d %d %d %d",
    (unsigned)a.x.color, (unsigned)a.x.bgcolor,
    (int)a.x.bold, (int)a.x.italic, (int)a.x.reverse, (int)a.x.underline);
}

/* Strings worth writing. */
static const char* writes[] = {
  "", "a", "hello", "line\nnext", "tab\there", "cr\rhere",
  "\x01\x02", "\x07" "bell", "\x08" "back", "\x0b\x0c", "\x1b", "\x1b[",
  "\x1b[m", "\x1b[31m", "\x1b[1;4mbold", "\x1b[0m", "\x1b]0;title\x07",
  "\xc3\xa9", "\xe6\x97\xa5\xe6\x9c\xac", "\xff", "a\xff" "b",
  "\xf0\x9f\x98\x80", "mixed \x1b[32mgreen\x1b[39m end"
};
#define NWRITES ((int)(sizeof(writes)/sizeof(writes[0])))

/* Environments worth testing for palette detection. */
static const char* envs[][3] = {
  /* COLORTERM, TERM, NO_COLOR */
  { NULL, NULL, NULL },
  { "truecolor", NULL, NULL },
  { "24bit", NULL, NULL },
  { "direct", NULL, NULL },
  { "8bit", NULL, NULL },
  { "256color", NULL, NULL },
  { "4bit", NULL, NULL },
  { "16color", NULL, NULL },
  { "3bit", NULL, NULL },
  { "8color", NULL, NULL },
  { "1bit", NULL, NULL },
  { "nocolor", NULL, NULL },
  { "monochrome", NULL, NULL },
  { NULL, "xterm", NULL },
  { NULL, "xterm-256color", NULL },
  { NULL, "xterm-truecolor", NULL },
  { NULL, "alacritty", NULL },
  { NULL, "kitty", NULL },
  { NULL, "gnome", NULL },
  { NULL, "screen-16color", NULL },
  { NULL, "vt100-8color", NULL },
  { NULL, "dumb", NULL },
  { NULL, "monochrome", NULL },
  { NULL, "linux", NULL },
  { "truecolor", "dumb", NULL },
  { NULL, NULL, "1" },
  { "truecolor", NULL, "1" },
  { "", "", NULL },
};
#define NENVS ((int)(sizeof(envs)/sizeof(envs[0])))

static void set_env(const char* colorterm, const char* term_name, const char* nocolor) {
  unsetenv("COLORTERM"); unsetenv("TERM"); unsetenv("NO_COLOR");
  unsetenv("WT_SESSION"); unsetenv("ITERM_SESSION_ID"); unsetenv("VSCODE_PID");
  unsetenv("COLUMNS"); unsetenv("LINES");
  if (colorterm != NULL) setenv("COLORTERM", colorterm, 1);
  if (term_name != NULL) setenv("TERM", term_name, 1);
  if (nocolor != NULL) setenv("NO_COLOR", nocolor, 1);
}

int main(void) {
  if (open_pty(80, 24) < 0) {
    fprintf(stderr, "cannot open a pseudo-terminal\n");
    return 1;
  }

  /* Palette detection. */
  for (int i = 0; i < NENVS; i++) {
    set_env(envs[i][0], envs[i][1], envs[i][2]);
    term_t* t = new_term();
    printf("palette %d %d %d %d\n", i, (int)t->palette, t->nocolor ? 1 : 0, term_get_color_bits(t));
    term_free(t);
    emit("discard");
  }

  /* term_is_interactive, which tests TERM against a list the wrong way round. */
  static const char* terms[] = {
    "dumb", "DUMB", "cons25", "emacs", "EMACS", "xterm", "", "b|DUMB",
    "umb", "25|CONS", "|", "dum", "screen"
  };
  for (int i = 0; i < (int)(sizeof(terms)/sizeof(terms[0])); i++) {
    set_env(NULL, terms[i], NULL);
    term_t* t = new_term();
    printf("interactive "); print_esc_bytes(terms[i], (ssize_t)strlen(terms[i]));
    printf(" %d\n", term_is_interactive(t) ? 1 : 0);
    term_free(t);
    emit("discard");
  }

  /* Writing. Each string on a fresh unbuffered terminal. */
  set_env(NULL, "xterm-256color", NULL);
  for (int i = 0; i < NWRITES; i++) {
    term_t* t = new_term();
    term_set_buffer_mode(t, UNBUFFERED);
    emit("discard");
    term_write(t, writes[i]);
    flush_emit(t, "write");
    printf("attrafter %d ", i); print_attr(term_get_attr(t)); printf("\n");
    term_free(t);
    emit("discard");
  }

  /* Cursor movement and line clearing. */
  {
    term_t* t = new_term();
    term_set_buffer_mode(t, UNBUFFERED);
    emit("discard");
    static const ssize_t ns[] = { -1, 0, 1, 2, 10, 999 };
    for (int i = 0; i < (int)(sizeof(ns)/sizeof(ns[0])); i++) {
      term_left(t, ns[i]);  flush_emit(t, "left");
      term_right(t, ns[i]); flush_emit(t, "right");
      term_up(t, ns[i]);    flush_emit(t, "up");
      term_down(t, ns[i]);  flush_emit(t, "down");
    }
    term_clear_line(t);             flush_emit(t, "clearline");
    term_clear_to_end_of_line(t);   flush_emit(t, "cleareol");
    term_start_of_line(t);          flush_emit(t, "startline");
    term_attr_reset(t);             flush_emit(t, "attrreset");
    term_underline(t, true);        flush_emit(t, "ul1");
    term_underline(t, false);       flush_emit(t, "ul0");
    term_reverse(t, true);          flush_emit(t, "rev1");
    term_reverse(t, false);         flush_emit(t, "rev0");
    term_bold(t, true);             flush_emit(t, "bold1");
    term_bold(t, false);            flush_emit(t, "bold0");
    term_italic(t, true);           flush_emit(t, "it1");
    term_italic(t, false);          flush_emit(t, "it0");
    term_writeln(t, "ln");          flush_emit(t, "writeln");
    term_write_char(t, 'x');        flush_emit(t, "writechar");
    term_write_char(t, '\n');       flush_emit(t, "writecharnl");
    term_write_repeat(t, "ab", 3);  flush_emit(t, "repeat3");
    term_write_repeat(t, "ab", 0);  flush_emit(t, "repeat0");
    term_write_repeat(t, "ab", -1); flush_emit(t, "repeatneg");
    printf("dim %d %d\n", (int)term_get_width(t), (int)term_get_height(t));
    term_free(t);
    emit("discard");
  }

  /* Setting attributes, which is where the C keeps stale state. */
  {
    static const char* seq[] = { "1", "1", "31", "31", "0", "4", "38;5;33", "38;2;1;2;3", "22", "" };
    term_t* t = new_term();
    term_set_buffer_mode(t, UNBUFFERED);
    emit("discard");
    for (int i = 0; i < (int)(sizeof(seq)/sizeof(seq[0])); i++) {
      attr_t a = attr_from_sgr(seq[i], (ssize_t)strlen(seq[i]));
      term_set_attr(t, a);
      flush_emit(t, "setattr");
      printf("attrstate %d ", i); print_attr(term_get_attr(t)); printf("\n");
    }
    term_free(t);
    emit("discard");
  }

  /* Formatted output, which drives set_attr from an array. */
  {
    term_t* t = new_term();
    term_set_buffer_mode(t, UNBUFFERED);
    emit("discard");
    attrbuf_t* ab = attrbuf_new(&probe_mem);
    stringbuf_t* sb = sbuf_new(&probe_mem);
    attrbuf_append_n(sb, ab, "red", 3, attr_from_sgr("31", 2));
    attrbuf_append_n(sb, ab, "plain", 5, attr_none());
    attrbuf_append_n(sb, ab, "bold", 4, attr_from_sgr("1", 1));
    const char* s = sbuf_string(sb);
    term_write_formatted(t, s, attrbuf_attrs(ab, sbuf_len(sb)));
    flush_emit(t, "formatted");
    term_write_formatted(t, s, NULL);
    flush_emit(t, "formattednull");
    sbuf_free(sb);
    attrbuf_free(ab);
    term_free(t);
    emit("discard");
  }

  /* Buffer modes. */
  {
    term_t* t = new_term();
    emit("discard");
    printf("bufmode %d\n", (int)term_set_buffer_mode(t, BUFFERED));
    term_write(t, "buffered");
    emit("buffered_nothing");
    term_flush(t);
    emit("buffered_flushed");
    printf("bufmode %d\n", (int)term_set_buffer_mode(t, LINEBUFFERED));
    term_write(t, "no newline");
    emit("line_nothing");
    term_write(t, " and\n");
    emit("line_flushed");
    printf("bufmode %d\n", (int)term_set_buffer_mode(t, UNBUFFERED));
    emit("unbuffered_switch");
    term_write(t, "direct");
    emit("unbuffered");
    term_free(t);
    emit("discard");
  }
  return 0;
}
