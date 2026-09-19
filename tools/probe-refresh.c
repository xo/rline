/* Print what isocline draws when it redraws a line, so that the Go port can be
   checked against it.

   The editor is filled in rather than started, the same way
   tools/probe-editline.c does it, and the terminal sits on a real
   pseudo-terminal so that the drawing goes somewhere real. */

#define _XOPEN_SOURCE 700
#define _DEFAULT_SOURCE

#include "isocline.c"

#include <stdio.h>
#include <string.h>
#include <stdlib.h>
#include <fcntl.h>
#include <poll.h>

static alloc_t probe_mem = { &malloc, &realloc, &free };
static int leader_fd = -1, follower_fd = -1;

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
  ws.ws_col = (unsigned short)cols; ws.ws_row = (unsigned short)rows;
  if (ioctl(follower_fd, TIOCSWINSZ, &ws) < 0) return -1;
  return 0;
}

static void print_esc(const char* s, ssize_t n) {
  if (n <= 0) { printf("-"); return; }
  for (ssize_t i = 0; i < n; i++) {
    unsigned char c = (unsigned char)s[i];
    if (c == 0x1b) printf("\\e");
    else if (c == '\\') printf("\\\\");
    else if (c == '\r') printf("\\r");
    else if (c == '\n') printf("\\n");
    else if (c == ' ') printf("\\s");
    else if (c < 0x20 || c >= 0x7f) printf("\\x%02x", c);
    else printf("%c", c);
  }
}

static void emit(void) {
  char buf[16384];
  ssize_t total = 0;
  for (;;) {
    struct pollfd p; p.fd = leader_fd; p.events = POLLIN;
    if (poll(&p, 1, 20) <= 0) break;
    ssize_t n = read(leader_fd, buf + total, sizeof(buf) - (size_t)total);
    if (n <= 0) break;
    total += n;
    if (total >= (ssize_t)sizeof(buf)) break;
  }
  print_esc(buf, total);
}

static ic_env_t env;
static editor_t eb;

int main(void) {
  if (open_pty(40, 6) < 0) { fprintf(stderr, "no pty\n"); return 1; }
  setenv("TERM", "xterm-256color", 1);
  unsetenv("COLORTERM"); unsetenv("NO_COLOR"); unsetenv("COLUMNS"); unsetenv("LINES");

  memset(&env, 0, sizeof(env));
  env.mem = &probe_mem;
  env.term = term_new(&probe_mem, NULL, false, true, follower_fd);
  env.tty = NULL;
  env.bbcode = bbcode_new(&probe_mem, env.term);
  env.prompt_marker = "> ";
  env.cprompt_marker = "| ";
  env.match_braces = "()[]{}";
  env.auto_braces = "()[]{}";
  env.multiline_eol = '\\';
  env.no_highlight = true;

  memset(&eb, 0, sizeof(eb));
  eb.input = sbuf_new(&probe_mem);
  eb.extra = sbuf_new(&probe_mem);
  eb.hint = sbuf_new(&probe_mem);
  eb.hint_help = sbuf_new(&probe_mem);
  eb.attrs = attrbuf_new(&probe_mem);
  eb.attrs_extra = attrbuf_new(&probe_mem);
  eb.mem = &probe_mem;

  static const char* texts[] = {
    "", "a", "hello", "hello world",
    "0123456789012345678901234567890123456789012345",
    "line1\nline2", "a\nb\nc\nd\ne\nf\ng",
    "(unbalanced", "[matched]", "\xe6\x97\xa5\xe6\x9c\xac"
  };
  static const char* prompts[] = { "", "ps" };
  static const char* hints[] = { "", "hint" };
  static const char* extras[] = { "", "menu line", "two\nlines" };

  int caseno = 0;
  for (int t = 0; t < (int)(sizeof(texts)/sizeof(texts[0])); t++) {
    const ssize_t len = (ssize_t)strlen(texts[t]);
    for (ssize_t pos = 0; pos <= len; pos += (len > 8 ? 7 : 1)) {
      for (int p = 0; p < 2; p++) {
        for (int h = 0; h < 2; h++) {
          for (int x = 0; x < 3; x++) {
            for (int nmi = 0; nmi < 2; nmi++) {
              for (int nbm = 0; nbm < 2; nbm++) {
                sbuf_replace(eb.input, texts[t]);
                sbuf_replace(eb.hint, hints[h]);
                sbuf_replace(eb.extra, extras[x]);
                sbuf_clear(eb.hint_help);
                eb.pos = pos;
                eb.termw = 40;
                eb.cur_rows = 1;
                eb.cur_row = 0;
                eb.prompt_text = prompts[p];
                env.no_multiline_indent = (nmi != 0);
                env.no_bracematch = (nbm != 0);
                term_flush(env.term);
                emit(); /* discard anything pending */
                printf("\n");
                edit_refresh(&env, &eb);
                term_flush(env.term);
                printf("refresh %d ", caseno++);
                emit();
                printf(" %d %d %d ", (int)eb.cur_rows, (int)eb.cur_row, (int)eb.pos);
                print_esc(sbuf_string(eb.input), sbuf_len(eb.input));
                printf("\n");
              }
            }
          }
        }
      }
    }
  }
  return 0;
}
