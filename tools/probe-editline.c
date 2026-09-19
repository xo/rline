/* Print the state that isocline's editing operations leave behind, so that the
   Go port can be checked against it.

   editor_t and ic_env_t are plain structures, so the probe fills them in
   rather than starting an editor. The operations redraw as they go, so the
   probe gives them a terminal on a real pseudo-terminal and throws the output
   away. This records what each operation does to the text, the cursor and the
   undo stacks. Redrawing is checked separately. */

#define _XOPEN_SOURCE 700
#define _DEFAULT_SOURCE

#include "isocline.c"

#include <stdio.h>
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

/* Empty the terminal, so it never fills and blocks. */
static void drain(void) {
  char buf[8192];
  for (;;) {
    struct pollfd p; p.fd = leader_fd; p.events = POLLIN;
    if (poll(&p, 1, 0) <= 0) break;
    if (read(leader_fd, buf, sizeof(buf)) <= 0) break;
  }
}

static void print_str(const char* s) {
  if (s == NULL || *s == 0) { printf("-"); return; }
  for (const unsigned char* p = (const unsigned char*)s; *p != 0; p++) {
    if (*p < 0x20 || *p >= 0x7f || *p == ' ' || *p == '\\') printf("\\x%02x", *p);
    else printf("%c", *p);
  }
}

static ssize_t stack_depth(editstate_t* es) {
  ssize_t n = 0;
  /* editstate_s is opaque here only by convention; the unity build exposes it.
     Walking it is the only way to see how deep the stack is. */
  for (editstate_t* p = es; p != NULL; p = p->next) n++;
  return n;
}

static ic_env_t env;
static editor_t eb;
static stringbuf_t *sb_input, *sb_extra, *sb_hint, *sb_hint_help;
static attrbuf_t *ab1, *ab2;

static void reset_editor(const char* text, ssize_t pos) {
  editstate_done(&probe_mem, &eb.undo);
  editstate_done(&probe_mem, &eb.redo);
  sbuf_replace(sb_input, text);
  sbuf_clear(sb_extra);
  sbuf_clear(sb_hint);
  sbuf_clear(sb_hint_help);
  eb.pos = pos;
  eb.cur_rows = 1;
  eb.cur_row = 0;
  eb.termw = 80;
  eb.modified = false;
  eb.disable_undo = false;
  eb.history_idx = 0;
}

static void report(const char* op, const char* text, ssize_t pos) {
  printf("op %s ", op);
  print_str(text);
  printf(" %d -> ", (int)pos);
  print_str(sbuf_string(eb.input));
  printf(" %d %d %d %d\n", (int)eb.pos, eb.modified ? 1 : 0,
         (int)stack_depth(eb.undo), (int)stack_depth(eb.redo));
  drain();
}

/* Inputs to run every operation against. */
static const char* texts[] = {
  "", "a", "hello", "hello world", "  spaced  out  ",
  "one two three", "line1\nline2", "a\nb\nc",
  "\xc3\xa9\xc3\xa9\xc3\xa9", "a\xe6\x97\xa5" "b",
  "(nested [brackets])", "trailing   ", "   leading"
};
#define NTEXTS ((int)(sizeof(texts)/sizeof(texts[0])))

int main(void) {
  if (open_pty(80, 24) < 0) { fprintf(stderr, "no pty\n"); return 1; }
  setenv("TERM", "xterm-256color", 1);
  unsetenv("COLORTERM"); unsetenv("NO_COLOR"); unsetenv("COLUMNS"); unsetenv("LINES");

  memset(&env, 0, sizeof(env));
  env.mem = &probe_mem;
  env.term = term_new(&probe_mem, NULL, false, true, follower_fd);
  env.tty = NULL;
  env.bbcode = bbcode_new(&probe_mem, env.term);
  env.history = history_new(&probe_mem);
  env.completions = completions_new(&probe_mem);
  env.prompt_marker = "> ";
  env.cprompt_marker = "> ";
  env.match_braces = "()[]{}";
  env.auto_braces = "()[]{}\"\"''";
  env.multiline_eol = '\\';
  env.singleline_only = false;
  env.no_hint = true;
  env.no_highlight = true;

  memset(&eb, 0, sizeof(eb));
  sb_input = sbuf_new(&probe_mem);
  sb_extra = sbuf_new(&probe_mem);
  sb_hint = sbuf_new(&probe_mem);
  sb_hint_help = sbuf_new(&probe_mem);
  ab1 = attrbuf_new(&probe_mem);
  ab2 = attrbuf_new(&probe_mem);
  eb.input = sb_input; eb.extra = sb_extra; eb.hint = sb_hint;
  eb.hint_help = sb_hint_help; eb.mem = &probe_mem;
  eb.attrs = ab1; eb.attrs_extra = ab2;
  eb.prompt_text = "";

  struct { const char* name; void (*fn)(ic_env_t*, editor_t*); } ops[] = {
    { "left",             &edit_cursor_left },
    { "right",            &edit_cursor_right },
    { "line-end",         &edit_cursor_line_end },
    { "line-start",       &edit_cursor_line_start },
    { "next-word",        &edit_cursor_next_word },
    { "prev-word",        &edit_cursor_prev_word },
    { "next-ws-word",     &edit_cursor_next_ws_word },
    { "prev-ws-word",     &edit_cursor_prev_ws_word },
    { "to-start",         &edit_cursor_to_start },
    { "to-end",           &edit_cursor_to_end },
    { "match-brace",      &edit_cursor_match_brace },
    { "backspace",        &edit_backspace },
    { "delete-char",      &edit_delete_char },
    { "delete-all",       &edit_delete_all },
    { "del-to-line-end",  &edit_delete_to_end_of_line },
    { "del-to-line-start",&edit_delete_to_start_of_line },
    { "delete-line",      &edit_delete_line },
    { "del-to-word-start",&edit_delete_to_start_of_word },
    { "del-to-word-end",  &edit_delete_to_end_of_word },
    { "del-to-ws-start",  &edit_delete_to_start_of_ws_word },
    { "del-to-ws-end",    &edit_delete_to_end_of_ws_word },
    { "delete-word",      &edit_delete_word },
    { "swap-char",        &edit_swap_char },
    { "multiline-eol",    &edit_multiline_eol },
  };
  const int nops = (int)(sizeof(ops)/sizeof(ops[0]));

  for (int t = 0; t < NTEXTS; t++) {
    const ssize_t len = (ssize_t)strlen(texts[t]);
    for (ssize_t pos = 0; pos <= len; pos++) {
      for (int o = 0; o < nops; o++) {
        reset_editor(texts[t], pos);
        drain();
        (ops[o].fn)(&env, &eb);
        report(ops[o].name, texts[t], pos);
      }
    }
  }

  /* Inserting, which is where auto braces and auto indent live. */
  static const char inserts[] = { 'x', '(', ')', '[', ']', '"', '\'', ' ', '\n' };
  for (int t = 0; t < NTEXTS; t++) {
    const ssize_t len = (ssize_t)strlen(texts[t]);
    for (ssize_t pos = 0; pos <= len; pos++) {
      for (int c = 0; c < (int)sizeof(inserts); c++) {
        reset_editor(texts[t], pos);
        drain();
        edit_insert_char(&env, &eb, inserts[c]);
        char name[32];
        snprintf(name, sizeof(name), "insert-%02x", (unsigned char)inserts[c]);
        report(name, texts[t], pos);
      }
    }
  }

  /* Undo and redo, which need a change to undo. */
  for (int t = 0; t < NTEXTS; t++) {
    reset_editor(texts[t], 0);
    drain();
    edit_insert_char(&env, &eb, 'Z');
    report("undo-setup", texts[t], 0);
    edit_undo_restore(&env, &eb);
    report("undo", texts[t], 0);
    edit_redo_restore(&env, &eb);
    report("redo", texts[t], 0);
    edit_undo_restore(&env, &eb);
    edit_undo_restore(&env, &eb);
    report("undo-twice", texts[t], 0);
  }
  return 0;
}
