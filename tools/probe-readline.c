/* Print what GNU readline does to a line for each key, so that rline's
   readline commands can be checked against it (D45, D46).

   This links the GNU readline installed on the system, through its public
   API, and copies nothing from it. readline is GPL, and this probe is a tool
   for recording: it is built into .build/ and is not part of the Go module.
   The recordings are facts about what readline does.

   readline runs through its callback interface, with no inputrc, so the
   keymap is readline's own default emacs keymap. Each case runs in a process
   of its own, so that it starts with an empty line, an empty kill ring and no
   previous command. The probe feeds the bytes of one key at a time. Nothing
   is drawn: the output goes to /dev/null, because rline draws differently and
   only the effect of a key is compared.

   The first line names the version of readline. Then each case prints its
   history, oldest first, as

       history <case> <entry in hex>

   the bindings it makes before it starts, for the commands that readline
   does not bind by default, as

       bind <case> <command> <key sequence as an inputrc file writes it, in hex>

   and one line for each key, as

       <case> <key in hex> <cursor> <mark> <accepted> <line in hex>

   where the cursor and the mark count bytes, accepted is 1 once readline
   hands the line back, and an empty line is -. A case that completes gives
   readline a list of words, and prints them first as

       words <case> <word in hex> ...

   and each list of completions that readline shows, before the line of the
   key that showed it, as

       list <case> <completion in hex> ...

   While readline shows a prompt of its own, as incremental search does, the
   line of a key is followed by

       prompt <case> <prompt in hex>

   A case can give readline lines of an inputrc file, which it reads before
   the keys and prints first, one to a line, as

       rc <case> <line in hex>

   A case that sets out records what readline writes to its output stream,
   as the commands that print do, and prints it after the line of the key
   that wrote it, as

       out <case> <text in hex>

   A case that sets again goes on after readline hands a line back, so that
   the next keys edit the next line, as operate-and-get-next needs.

   A few commands read keys themselves and do nothing through the callback
   interface: execute-named-command, operate-and-get-next and
   arrow-key-prefix. A case that sets blocking runs readline() instead, as
   bash does, and prints the state after a key when readline asks for the
   byte after it. The first key of most cases
   is the text the case starts from, fed whole, and its key is printed as -.
   The kill ring is not printed, because the shared library does not export
   it, so the cases yank to show what it holds.

   With the argument binds, the probe prints every command readline has, as

       command <name>

   and then the default keymaps, one binding to a line, as

       <keymap> <command> <key sequence as an inputrc file writes it> */

#define _DEFAULT_SOURCE
#define _XOPEN_SOURCE 700

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <locale.h>
#include <unistd.h>
#include <sys/wait.h>

#include <readline/readline.h>
#include <readline/history.h>

/* The bytes of the key being fed, and how far readline has read them. */
static const char *feed;
static size_t feed_len, feed_pos;

/* getc gives readline the next byte of the key being fed. It never runs past
   the key, because the probe calls rl_callback_read_char only while bytes
   are left. */
static int getc_feed(FILE *f) {
  (void)f;
  if (feed_pos >= feed_len) return EOF;
  return (unsigned char)feed[feed_pos++];
}

/* done is set when readline hands back a line. */
static int done;

static void handler(char *line) {
  done = 1;
  free(line);
}

/* The terminal is never touched: there is none. */
static void prep(int meta) { (void)meta; }
static void deprep(void) {}

/* print_bytes writes a string as hex, or - for an empty one, so that every
   field is one word. */
static void print_bytes(const char *s) {
  if (s == NULL || *s == 0) {
    printf("-");
    return;
  }
  for (const unsigned char *p = (const unsigned char *)s; *p; p++) printf("%02x", *p);
}

/* reset puts readline back to an empty line, with no history and no undo.
   Each case runs in a process of its own as well, so that the kill ring and
   the previous command, which the API cannot clear, start empty. */
static void reset(void) {
  rl_callback_handler_remove();
  clear_history();
  done = 0;
  rl_callback_handler_install("", handler);
  rl_free_undo_list();
}

/* capture is set while a case records the output. */
static int capture;

/* feed_key feeds the bytes of one key to readline. In a case that records
   the output, standard output goes to the same file while readline reads,
   because print-last-kbd-macro writes the macro there. */
static void feed_key(const char *key, size_t n) {
  int saved = -1;
  if (capture) {
    fflush(stdout);
    fflush(rl_outstream);
    saved = dup(1);
    dup2(fileno(rl_outstream), 1);
  }
  feed = key;
  feed_len = n;
  feed_pos = 0;
  while (feed_pos < feed_len && !done) rl_callback_read_char();
  if (saved >= 0) {
    fflush(stdout);
    dup2(saved, 1);
    close(saved);
  }
}

/* print_state prints the line of one key, and the prompt readline shows. */
static void print_state(const char *name, const char *key, size_t n) {
  printf("%s ", name);
  if (n == 0) printf("-");
  for (size_t i = 0; i < n; i++) printf("%02x", (unsigned char)key[i]);
  printf(" %d %d %d ", rl_point, rl_mark, done);
  print_bytes(done ? NULL : rl_line_buffer);
  printf("\n");
  if (!done && rl_display_prompt != NULL && rl_display_prompt[0] != 0) {
    printf("prompt %s ", name);
    print_bytes(rl_display_prompt);
    printf("\n");
  }
}

/* step feeds one key and prints the state after it. */
static void step(const char *name, const char *key, size_t n) {
  feed_key(key, n);
  print_state(name, key, n);
}

/* written is how much of the output stream has been printed. */
static long written;

/* print_out prints what readline wrote to its output stream since the last
   key, when it wrote anything. */
static void print_out(const char *name) {
  fflush(rl_outstream);
  long end = ftell(rl_outstream);
  if (end <= written) return;
  fseek(rl_outstream, written, SEEK_SET);
  printf("out %s ", name);
  for (long i = written; i < end; i++) printf("%02x", (unsigned char)fgetc(rl_outstream));
  printf("\n");
  fseek(rl_outstream, end, SEEK_SET);
  written = end;
}

/* A case is a name, the history it starts with, and its keys. Each key is a
   string, so that a key of several bytes, such as ESC b, is fed whole. */
struct probe_case {
  const char *name;
  const char *history[8];
  const char *keys[40];
  /* Key sequences and the commands to bind them to before the case runs, in
     pairs, as an inputrc file writes them. They reach the commands that
     readline does not bind by default. */
  const char *binds[8];
  /* The words that completion offers. A case without them never completes. */
  const char *words[12];
  /* Lines of an inputrc file, which readline reads before the keys. */
  const char *rc[6];
  /* out records what readline writes, and again goes on to the next line
     after readline hands a line back. */
  int out;
  int again;
  int blocking;
};

/* The case that is running, for the completion functions and for blocking
   cases. */
static const struct probe_case *running;

/* complete_word offers each word of the running case that starts with text,
   as a completion entry function does: state is 0 on the first call. */
static char *complete_word(const char *text, int state) {
  static int next;
  if (state == 0) next = 0;
  size_t n = strlen(text);
  while (running->words[next] != NULL) {
    const char *w = running->words[next++];
    if (strncmp(w, text, n) == 0) return strdup(w);
  }
  return NULL;
}

/* show_list prints the completions that readline would show. matches[0] is
   what they share, and the completions follow it. */
static void show_list(char **matches, int len, int max) {
  (void)max;
  printf("list %s", running->name);
  for (int i = 1; i <= len; i++) {
    printf(" ");
    print_bytes(matches[i]);
  }
  printf("\n");
}

/* The texts the commands run over: words, punctuation, runs of spaces, and
   characters of more than one byte. */
#define T1 "hello world, foo-bar  baz_qux 42"
#define T2 "h\xc3\xa9llo w\xc3\xb6rld \xe2\x82\xac" "9"

static const struct probe_case cases[] = {
  /* Moving. */
  {"bol", {0}, {T1, "\x01"}},
  {"eol", {0}, {T1, "\x01", "\x05"}},
  {"fchar", {0}, {T1, "\x01", "\x06", "\x06", "\x06"}},
  {"bchar", {0}, {T1, "\x02", "\x02", "\x02"}},
  {"fword", {0}, {T1, "\x01", "\x1b" "f", "\x1b" "f", "\x1b" "f", "\x1b" "f", "\x1b" "f", "\x1b" "f", "\x1b" "f"}},
  {"bword", {0}, {T1, "\x1b" "b", "\x1b" "b", "\x1b" "b", "\x1b" "b", "\x1b" "b", "\x1b" "b", "\x1b" "b"}},
  {"fword-mb", {0}, {T2, "\x01", "\x1b" "f", "\x1b" "f", "\x1b" "f", "\x1b" "f"}},
  {"bword-mb", {0}, {T2, "\x1b" "b", "\x1b" "b", "\x1b" "b", "\x1b" "b"}},
  {"fchar-mb", {0}, {T2, "\x01", "\x06", "\x06", "\x06"}},
  {"bchar-mb", {0}, {T2, "\x02", "\x02", "\x02"}},

  /* Deleting and killing. */
  {"delete-char", {0}, {T1, "\x01", "\x04", "\x04"}},
  {"backward-delete-char", {0}, {T1, "\x7f", "\x7f", "\x08"}},
  {"delete-mb", {0}, {T2, "\x01", "\x06", "\x04", "\x7f"}},
  {"kill-line", {0}, {T1, "\x01", "\x1b" "f", "\x0b"}},
  {"kill-line-twice", {0}, {T1, "\x01", "\x0b", "\x0b"}},
  {"unix-line-discard", {0}, {T1, "\x1b" "b", "\x15"}},
  {"unix-word-rubout", {0}, {T1, "\x17", "\x17", "\x17"}},
  {"kill-word", {0}, {T1, "\x01", "\x1b" "d", "\x1b" "d", "\x1b" "d"}},
  {"backward-kill-word", {0}, {T1, "\x1b" "\x7f", "\x1b" "\x7f", "\x1b" "\x08"}},
  {"kill-appends", {0}, {T1, "\x01", "\x1b" "d", "\x1b" "d", "\x05", "\x17", "\x17"}},
  /* M-\\ deletes spaces in bash, and readline leaves it unbound. */
  {"delete-horizontal-space-unbound", {0}, {T1, "\x1b" "b", "\x1b" "b", "\x02", "\x1b" "\\"}},

  /* Yanking. */
  {"yank", {0}, {T1, "\x17", "\x01", "\x19"}},
  {"yank-pop", {0}, {T1, "\x17", "\x17", "\x01", "\x19", "\x1b" "y", "\x1b" "y"}},
  {"yank-twice", {0}, {T1, "\x01", "\x1b" "d", "\x05", "\x19", "\x19"}},
  {"yank-pop-rotate", {0}, {T1, "\x17", "\x02", "\x17", "\x01", "\x19", "\x1b" "y", "\x1b" "y", "\x1b" "y"}},
  {"yank-pop-not-after-yank", {0}, {T1, "\x17", "\x01", "\x1b" "y"}},
  /* Twelve separate kills, each broken from the next by C-b, then a yank and
     rotations, to show how many kills the ring keeps. */
  {"kill-ring-size", {0}, {"a b c d e f g h i j k l m", "\x17", "\x02", "\x17", "\x02", "\x17", "\x02", "\x17", "\x02", "\x17", "\x02", "\x17", "\x02", "\x17", "\x02", "\x17", "\x02", "\x17", "\x02", "\x17", "\x02", "\x17", "\x02", "\x17", "\x02", "\x19", "\x1b" "y", "\x1b" "y", "\x1b" "y", "\x1b" "y", "\x1b" "y", "\x1b" "y", "\x1b" "y", "\x1b" "y", "\x1b" "y", "\x1b" "y", "\x1b" "y", "\x1b" "y"}},

  /* Transposing and changing case. */
  {"transpose-chars", {0}, {T1, "\x01", "\x14", "\x06", "\x06", "\x14", "\x05", "\x14"}},
  {"transpose-words", {0}, {T1, "\x01", "\x1b" "f", "\x1b" "t", "\x1b" "t", "\x05", "\x1b" "t"}},
  {"upcase-word", {0}, {T1, "\x01", "\x1b" "u", "\x1b" "u", "\x1b" "b", "\x1b" "u"}},
  {"downcase-word", {0}, {"HELLO World FOO", "\x01", "\x1b" "l", "\x1b" "l"}},
  {"capitalize-word", {0}, {T1, "\x01", "\x1b" "c", "\x1b" "c", "\x1b" "c"}},
  {"case-mb", {0}, {T2, "\x01", "\x1b" "u", "\x1b" "c"}},

  /* Numeric arguments. */
  {"arg-fchar", {0}, {T1, "\x01", "\x1b" "3", "\x06"}},
  {"arg-fword", {0}, {T1, "\x01", "\x1b" "2", "\x1b" "f"}},
  {"arg-negative-fword", {0}, {T1, "\x1b" "-", "\x1b" "f"}},
  {"arg-kill-word", {0}, {T1, "\x01", "\x1b" "2", "\x1b" "d"}},
  {"arg-two-digits", {0}, {T1, "\x01", "\x1b" "1", "2", "\x06"}},
  {"arg-self-insert", {0}, {"ab", "\x1b" "4", "x"}},
  {"arg-delete-char", {0}, {T1, "\x01", "\x1b" "3", "\x04"}},
  /* With an argument, deleting characters kills them: the yank shows it. */
  {"arg-delete-char-yank", {0}, {T1, "\x01", "\x1b" "3", "\x04", "\x05", "\x19"}},
  {"arg-backward-delete-char-yank", {0}, {T1, "\x1b" "2", "\x7f", "\x01", "\x19"}},
  {"arg-delete-char-mark", {0}, {T1, "\x1b" "b", "\x1b" "2", "\x04"}},
  {"kill-word-mark", {0}, {T1, "\x1b" "b", "\x1b" "b", "\x1b" "d"}},
  {"arg-negative-kill-line", {0}, {T1, "\x1b" "b", "\x1b" "-", "\x0b"}},

  /* Undo. */
  {"undo-typing", {0}, {"abc def", "\x1f", "\x1f"}},
  {"undo-kill", {0}, {T1, "\x17", "\x1f"}},
  {"undo-yank", {0}, {T1, "\x17", "\x19", "\x1f"}},
  {"undo-transpose", {0}, {T1, "\x01", "\x06", "\x14", "\x1f"}},
  {"undo-xu", {0}, {"abc", "\x18" "\x15"}},
  {"undo-long-typing", {0}, {"abcdefghijklmnopqrstuvwxy", "\x1f", "\x1f", "\x1f"}},
  /* Fifteen characters of two bytes each, to show whether the limit on
     joining typed characters into one undo counts bytes or characters. */
  {"undo-long-typing-mb", {0}, {"\xc3\xa9\xc3\xa9\xc3\xa9\xc3\xa9\xc3\xa9\xc3\xa9\xc3\xa9\xc3\xa9\xc3\xa9\xc3\xa9\xc3\xa9\xc3\xa9\xc3\xa9\xc3\xa9\xc3\xa9", "\x1f", "\x1f"}},
  {"undo-typing-after-move", {0}, {"abc", "\x02", "x", "y", "\x1f", "\x1f"}},
  {"undo-kill-word", {0}, {T1, "\x01", "\x1b" "d", "\x1b" "d", "\x1f", "\x1f"}},
  {"revert-line", {0}, {T1, "\x17", "\x17", "\x1b" "r"}},

  /* Mark. */
  {"set-mark-exchange", {0}, {T1, "\x01", "\x1b" "f", "\x00", "\x05", "\x18" "\x18", "\x18" "\x18"}},

  /* Quoting and tabs. */
  {"quoted-insert", {0}, {"ab", "\x16", "\x01"}},
  {"tab-insert", {0}, {"ab", "\x1b" "\t"}},

  /* History. */
  {"previous-history", {"first", "second", "third", 0}, {"\x10", "\x10", "\x10", "\x10"}},
  {"next-history", {"first", "second", 0}, {"\x10", "\x10", "\x0e", "\x0e"}},
  {"history-edit-kept", {"first", "second", 0}, {"\x10", "x", "\x10", "\x0e"}},
  {"beginning-of-history", {"first", "second", "third", 0}, {"\x1b" "<", "\x1b" ">"}},
  {"yank-last-arg", {"select a from b", "insert c d", 0}, {"\x1b" ".", "\x1b" "."}},
  {"yank-nth-arg", {"select a from b", 0}, {"\x1b" "2", "\x1b" "\x19"}},

  /* Searching for a character. */
  {"character-search", {0}, {T1, "\x01", "\x1d", "o", "\x1d", "o", "\x1b" "2", "\x1d", "a"}},
  {"character-search-missing", {0}, {T1, "\x01", "\x1d", "Z"}},
  {"character-search-backward", {0}, {T1, "\x1b" "\x1d", "o", "\x1b" "\x1d", "o"}},

  /* Keyboard macros. */
  {"kbd-macro", {0}, {"\x18" "(", "a", "b", "\x02", "\x18" ")", "\x18" "e", "\x18" "e"}},

  /* Tilde expansion, with HOME set to /home/probe. */
  {"tilde-expand", {0}, {"ls ~/x", "\x1b" "~"}},
  {"tilde-expand-amp", {0}, {"cd ~", "\x1b" "&"}},

  /* Commands that readline does not bind by default. */
  {"kill-region", {0}, {T1, "\x01", "\x1b" "f", "\x00", "\x1b" "f", "\x1b" "f", "\x18" "r", "\x01", "\x19"}, {"\\C-xr", "kill-region"}},
  {"copy-region-as-kill", {0}, {T1, "\x01", "\x00", "\x1b" "f", "\x18" "w", "\x05", "\x19"}, {"\\C-xw", "copy-region-as-kill"}},
  {"copy-backward-word", {0}, {T1, "\x18" "b", "\x18" "b", "\x01", "\x19"}, {"\\C-xb", "copy-backward-word"}},
  {"copy-forward-word", {0}, {T1, "\x01", "\x18" "f", "\x18" "f", "\x05", "\x19"}, {"\\C-xf", "copy-forward-word"}},
  {"kill-whole-line", {0}, {T1, "\x1b" "b", "\x18" "k", "\x19"}, {"\\C-xk", "kill-whole-line"}},
  {"forward-backward-delete-char", {0}, {T1, "\x01", "\x18" "d", "\x05", "\x18" "d"}, {"\\C-xd", "forward-backward-delete-char"}},
  {"unix-filename-rubout", {0}, {"ls /usr/local/bin", "\x18" "u", "\x18" "u", "\x18" "u"}, {"\\C-xu", "unix-filename-rubout"}},
  {"delete-horizontal-space", {0}, {"a   b", "\x02", "\x02", "\x18" "h"}, {"\\C-xh", "delete-horizontal-space"}},
  {"overwrite-mode", {0}, {"abcdef", "\x01", "\x18" "o", "X", "Y", "\x7f", "\x18" "o", "Z"}, {"\\C-xo", "overwrite-mode"}},
  {"forward-byte", {0}, {T2, "\x01", "\x18" ">", "\x18" ">", "\x18" "<"}, {"\\C-x>", "forward-byte", "\\C-x<", "backward-byte"}},
  {"history-search-backward", {"select 1", "show tables", "select 2", 0}, {"sel", "\x18" "p", "\x18" "p", "\x18" "p", "\x18" "n"}, {"\\C-xp", "history-search-backward", "\\C-xn", "history-search-forward"}},
  {"history-substring-search", {"select 1", "show tables", "select 2", 0}, {"ab", "\x7f", "\x7f", "le", "\x18" "p", "\x18" "p", "\x18" "n"}, {"\\C-xp", "history-substring-search-backward", "\\C-xn", "history-substring-search-forward"}},

  /* Completion, from a fixed list of words. */
#define W {"select", "selection", "set", "show", "shutdown", "drop", 0}
  {"complete-one", {0}, {"sho", "\t", "x"}, .words = W},
  {"complete-shared", {0}, {"sel", "\t", "\t", "\t"}, .words = W},
  {"complete-none", {0}, {"xyz", "\t", "\t"}, .words = W},
  {"complete-ambiguous", {0}, {"s", "\t", "\t"}, .words = W},
  {"complete-empty", {0}, {"\t", "\t"}, .words = W},
  {"complete-exact", {0}, {"set", "\t"}, .words = W},
  {"complete-exact-longer", {0}, {"select", "\t", "\t"}, .words = W},
  {"complete-midline", {0}, {"sho tables", "\x01", "\x06", "\x06", "\x06", "\t"}, .words = W},
  {"complete-midline-shared", {0}, {"sel x", "\x01", "\x06", "\x06", "\x06", "\t"}, .words = W},
  {"complete-word-break", {0}, {"x(sho", "\t"}, .words = W},
  {"complete-midword", {0}, {"shox", "\x02", "\t"}, .words = W},
  {"complete-midline-end-space", {0}, {"sho ", "\x02", "\t"}, .words = W},
  {"complete-twice-listed", {0}, {"s", "\t", "\t", "\t", "\t"}, .words = W},
  {"complete-after-space", {0}, {"drop sh", "\t", "\t"}, .words = W},
  {"possible-completions", {0}, {"s", "\x1b" "?", "\x1b" "?"}, .words = W},
  {"insert-completions", {0}, {"se", "\x1b" "*"}, .words = W},
  {"complete-then-type", {0}, {"sel", "\t", "e", "\t", "\t"}, .words = W},
  {"menu-complete", {0}, {"se", "\x18" "m", "\x18" "m", "\x18" "m", "\x18" "m", "\x18" "m"},
   {"\\C-xm", "menu-complete"}, .words = W},
  {"menu-complete-backward", {0}, {"se", "\x18" "n", "\x18" "n", "\x18" "n", "\x18" "m"},
   {"\\C-xm", "menu-complete", "\\C-xn", "menu-complete-backward"}, .words = W},
  {"menu-complete-one", {0}, {"sho", "\x18" "m", "\x18" "m"}, {"\\C-xm", "menu-complete"}, .words = W},
  {"menu-complete-then-type", {0}, {"se", "\x18" "m", "\x18" "m", "x"}, {"\\C-xm", "menu-complete"}, .words = W},
  {"complete-undo", {0}, {"sel", "\t", "\x1f"}, .words = W},
#undef W

  /* Incremental search. */
#define H {"select 1", "show tables", "select 2", "drop x"}
  {"isearch", H, {"\x12", "s", "e", "l", "\r"}},
  {"isearch-again", H, {"\x12", "s", "e", "l", "\x12", "\x12", "\x12", "\r"}},
  {"isearch-forward", H, {"\x12", "s", "e", "l", "\x12", "\x13", "\x13", "\r"}},
  {"isearch-inside", H, {"\x12", "t", "a", "b", "\r"}},
  {"isearch-rubout", H, {"\x12", "s", "h", "o", "\x7f", "\x7f", "\x7f", "\x7f", "\r"}},
  {"isearch-rubout-after-again", H, {"\x12", "s", "e", "l", "\x12", "\x7f", "\r"}},
  {"isearch-abort", H, {"abc", "\x12", "s", "e", "l", "\x07", "x"}},
  {"isearch-abort-moved", H, {"abc", "\x02", "\x12", "s", "e", "l", "\x07", "x"}},
  {"isearch-other-key", H, {"\x12", "s", "e", "l", "\x01", "x"}},
  {"isearch-escape", H, {"\x12", "t", "a", "b", "\x1b", "x"}},
  {"isearch-ctrl-j", H, {"\x12", "t", "a", "b", "\n", "x"}},
  {"isearch-fails", H, {"\x12", "z", "q", "\x7f", "\r"}},
  {"isearch-empty-again", H, {"\x12", "\x12", "\x12", "\r"}},
  {"isearch-same-again", H, {"\x12", "s", "e", "l", "\r", "\x12", "\x12"}},
  {"isearch-from-text", H, {"tab", "\x12", "\x12", "\r"}},
  {"isearch-no-history", {0}, {"\x12", "a", "\r"}},
  {"isearch-forward-start", H, {"\x13", "s", "e", "l", "\r"}},
  {"isearch-undo", H, {"\x12", "s", "h", "o", "\x1b", "\x1f"}},
  {"isearch-first-newest", H, {"\x12", "d", "\r"}},
  {"isearch-first-inside", H, {"\x12", "x", "\r"}},
  {"isearch-first-middle", H, {"\x12", "e", "\r"}},
  {"isearch-first-again", H, {"\x12", "s", "\x12", "\x12", "\r"}},
  {"isearch-first-escape", H, {"\x12", "e", "\x1b", "x"}},
#undef H

  /* Lines of an inputrc file. Each line is read on its own, and set keymap
     then changes the keymap in use, which reading a whole file would put
     back afterwards, so a case that sets it sets it back. */
#define W {"select", "selection", "set", "show", "shutdown", "drop", 0}
  {"rc-show-all-if-ambiguous", {0}, {"s", "\t", "l", "\t"}, .words = W, .rc = {"set show-all-if-ambiguous on"}},
  {"rc-show-all-if-ambiguous-insert", {0}, {"sel", "\t", "\t"}, .words = W, .rc = {"set show-all-if-ambiguous on"}},
  {"rc-show-all-if-unmodified", {0}, {"se", "\t", "l", "\t", "\t"}, .words = W, .rc = {"set show-all-if-unmodified on"}},
  {"rc-disable-completion", {0}, {"sho", "\t"}, .words = W, .rc = {"set disable-completion on"}},
  {"rc-macro", {0}, {"\x18" "a", "\x18" "a"}, .rc = {"\"\\C-xa\": \"hello \""}},
  {"rc-macro-keys", {0}, {"abc", "\x18" "b"}, .rc = {"\"\\C-xb\": \"\\C-a> \""}},
  {"rc-bind", {0}, {"one two", "\x18" "x"}, .rc = {"\"\\C-xx\": backward-kill-word"}},
  {"rc-bind-sequence", {0}, {"abc", "\x1b[A"}, .rc = {"\"\\e[A\": beginning-of-line"}},
  {"rc-keymap-ctlx", {0}, {"abc", "\x01", "\x18" "y"}, .rc = {"set keymap emacs-ctlx", "\"y\": upcase-word", "set keymap emacs"}},
  {"rc-keymap-meta", {0}, {"abc", "\x01", "\x1b" "q"}, .rc = {"set keymap emacs-meta", "\"q\": capitalize-word", "set keymap emacs"}},
  {"rc-if-app", {0}, {"abc", "\x18" "z"}, .rc = {"$if rline-probe", "\"\\C-xz\": unix-line-discard", "$endif"}},
  {"rc-if-other-app", {0}, {"abc", "\x18" "z"}, .rc = {"$if other", "\"\\C-xz\": unix-line-discard", "$endif"}},
  {"rc-isearch-terminators", {"select 1", "show tables"}, {"\x12", "s", "e", "x", "y"}, .rc = {"set isearch-terminators x"}},
  {"rc-unbind", {0}, {"abc", "\x01"}, .rc = {"\"\\C-a\": self-insert"}},
#undef W

  /* vi mode, which an inputrc line turns on. readline starts it in insert
     mode, and Esc moves to command mode. */
#define VI .rc = {"set editing-mode vi"}
#define T3 "one two three"
  {"vi-insert", {0}, {"abc"}, VI},
  {"vi-esc", {0}, {"abc", "\x1b"}, VI},
  {"vi-esc-empty", {0}, {"\x1b", "x"}, VI},
  {"vi-h-l", {0}, {"abc", "\x1b", "h", "h", "h", "l", "l", "l"}, VI},
  {"vi-0-dollar-caret", {0}, {"  one two", "\x1b", "0", "$", "^"}, VI},
  {"vi-w", {0}, {T1, "\x1b", "0", "w", "w", "w", "w", "w", "w", "w", "w", "w", "w"}, VI},
  {"vi-b", {0}, {T1, "\x1b", "b", "b", "b", "b", "b", "b", "b", "b", "b"}, VI},
  {"vi-e", {0}, {T1, "\x1b", "0", "e", "e", "e", "e", "e", "e", "e", "e", "e"}, VI},
  {"vi-W-B-E", {0}, {T1, "\x1b", "0", "W", "W", "W", "E", "E", "B", "B"}, VI},
  {"vi-x-X", {0}, {"abcdef", "\x1b", "0", "x", "x", "X", "l", "X", "$", "x", "x"}, VI},
  {"vi-count", {0}, {T1, "\x1b", "0", "3", "w", "2", "x", "1", "2", "l"}, VI},
  {"vi-dw", {0}, {T3, "\x1b", "0", "d", "w", "d", "w", "d", "w"}, VI},
  {"vi-dd", {0}, {T3, "\x1b", "d", "d"}, VI},
  {"vi-D", {0}, {T3, "\x1b", "0", "w", "D"}, VI},
  {"vi-d-dollar", {0}, {T3, "\x1b", "0", "w", "d", "$"}, VI},
  {"vi-db", {0}, {T3, "\x1b", "d", "b", "d", "b"}, VI},
  {"vi-de", {0}, {T3, "\x1b", "0", "d", "e"}, VI},
  {"vi-d2w", {0}, {T3, "\x1b", "0", "d", "2", "w"}, VI},
  {"vi-2dw", {0}, {T3, "\x1b", "0", "2", "d", "w"}, VI},
  {"vi-dh-dl", {0}, {"abcdef", "\x1b", "h", "d", "h", "0", "d", "l"}, VI},
  {"vi-d0", {0}, {T3, "\x1b", "b", "d", "0"}, VI},
  {"vi-dt-df", {0}, {T3, "\x1b", "0", "d", "t", "w", "d", "f", "e"}, VI},
  {"vi-cw", {0}, {T3, "\x1b", "0", "c", "w", "X", "\x1b"}, VI},
  {"vi-cw-space", {0}, {"one  two", "\x1b", "0", "e", "l", "c", "w", "X", "\x1b"}, VI},
  {"vi-cc", {0}, {T3, "\x1b", "c", "c", "new", "\x1b"}, VI},
  {"vi-C", {0}, {T3, "\x1b", "0", "w", "C", "X", "\x1b"}, VI},
  {"vi-yw-p", {0}, {T3, "\x1b", "0", "y", "w", "$", "p"}, VI},
  {"vi-yw-P", {0}, {T3, "\x1b", "0", "y", "w", "$", "P"}, VI},
  {"vi-yy-p", {0}, {"ab", "\x1b", "y", "y", "p"}, VI},
  {"vi-x-p", {0}, {"abc", "\x1b", "0", "x", "p"}, VI},
  {"vi-dw-P", {0}, {T3, "\x1b", "0", "d", "w", "w", "P"}, VI},
  {"vi-r", {0}, {"abc", "\x1b", "0", "r", "X", "l", "2", "r", "Y"}, VI},
  {"vi-s", {0}, {"abc", "\x1b", "0", "s", "Z", "\x1b"}, VI},
  {"vi-S", {0}, {"abc", "\x1b", "S", "Z", "\x1b"}, VI},
  {"vi-tilde", {0}, {"aBc", "\x1b", "0", "~", "~", "~"}, VI},
  {"vi-R", {0}, {"abcdef", "\x1b", "0", "R", "X", "Y", "\x1b"}, VI},
  {"vi-R-past-end", {0}, {"ab", "\x1b", "0", "R", "X", "Y", "Z", "\x1b"}, VI},
  {"vi-f-t", {0}, {T1, "\x1b", "0", "f", "o", ";", ";", ",", "t", "w", "F", "e", "T", "l"}, VI},
  {"vi-u-insert", {0}, {"abc", "\x1b", "u"}, VI},
  {"vi-u-x", {0}, {"abc", "\x1b", "0", "x", "x", "u", "u"}, VI},
  {"vi-u-u", {0}, {"abc", "\x1b", "0", "x", "u", "u"}, VI},
  {"vi-u-second-insert", {0}, {"abc", "\x1b", "a", "XY", "\x1b", "u", "u"}, VI},
  {"vi-u-cw", {0}, {"one two", "\x1b", "0", "c", "w", "Z", "\x1b", "u"}, VI},
  {"vi-u-after-esc-only", {0}, {"\x1b", "i", "abc", "\x1b", "u"}, VI},
  {"vi-redo-x", {0}, {T3, "\x1b", "0", "x", ".", "."}, VI},
  {"vi-redo-dw", {0}, {T3, "\x1b", "0", "d", "w", "."}, VI},
  {"vi-redo-insert", {0}, {"ab", "\x1b", "0", "i", "XY", "\x1b", "l", "."}, VI},
  {"vi-match", {0}, {"(a[b]c)", "\x1b", "0", "%", "%", "l", "%"}, VI},
  {"vi-column", {0}, {T1, "\x1b", "5", "|", "|", "4", "0", "|"}, VI},
  {"vi-a", {0}, {"abc", "\x1b", "0", "a", "X", "\x1b"}, VI},
  {"vi-A", {0}, {"abc", "\x1b", "0", "A", "Y", "\x1b"}, VI},
  {"vi-I", {0}, {"  abc", "\x1b", "I", "Z", "\x1b"}, VI},
  {"vi-i", {0}, {"abc", "\x1b", "0", "l", "i", "W", "\x1b"}, VI},
  {"vi-history", {"first", "second", 0}, {"\x1b", "k", "k", "k", "j", "j"}, VI},
  {"vi-insert-ctrl-w", {0}, {"one two", "\x17"}, VI},
  {"vi-insert-backspace", {0}, {"abc", "\x7f", "\x08"}, VI},
  {"vi-eof", {0}, {"\x04"}, VI},
  {"vi-enter", {0}, {"abc", "\x1b", "0", "\r"}, VI},
  {"vi-emacs", {0}, {"abc", "\x1b", "\x05", "\x01", "X"}, VI},
  /* vi commands that rline added later. VIRC binds keys in command mode,
     and leaves the keymap of insert mode in use. */
#define VIRC(...) .rc = {"set editing-mode vi", "set keymap vi-command", __VA_ARGS__, "set keymap vi-insert"}
#define H3 {"first", "second", "third", 0}
#define HS {"select 1", "show tables", "select 2", 0}
#define W {"select", "selection", "set", "show", "shutdown", "drop", 0}
  {"vi-fword", {0}, {T1, "\x1b", "0", "g", "g", "g", "g", "g", "g", "g"}, VIRC("\"g\": vi-fword")},
  {"vi-fWord", {0}, {T1, "\x1b", "0", "g", "g", "g", "g", "g"}, VIRC("\"g\": vi-fWord")},
  {"vi-bword", {0}, {T1, "\x1b", "g", "g", "g", "g", "g", "g", "g"}, VIRC("\"g\": vi-bword")},
  {"vi-bWord", {0}, {T1, "\x1b", "g", "g", "g", "g", "g"}, VIRC("\"g\": vi-bWord")},
  {"vi-eword", {0}, {T1, "\x1b", "0", "g", "g", "g", "g", "g", "g", "g"}, VIRC("\"g\": vi-eword")},
  {"vi-eWord", {0}, {T1, "\x1b", "0", "g", "g", "g", "g", "g"}, VIRC("\"g\": vi-eWord")},
  {"vi-forward-word", {0}, {T1, "\x1b", "0", "g", "g", "g", "g", "g", "g", "g"}, VIRC("\"g\": vi-forward-word")},
  {"vi-forward-bigword", {0}, {T1, "\x1b", "0", "g", "g", "g", "g", "g"}, VIRC("\"g\": vi-forward-bigword")},
  {"vi-backward-word", {0}, {T1, "\x1b", "g", "g", "g", "g", "g", "g", "g"}, VIRC("\"g\": vi-backward-word")},
  {"vi-backward-bigword", {0}, {T1, "\x1b", "g", "g", "g", "g", "g"}, VIRC("\"g\": vi-backward-bigword")},
  {"vi-end-bigword", {0}, {T1, "\x1b", "0", "g", "g", "g", "g", "g"}, VIRC("\"g\": vi-end-bigword")},
  {"vi-word-count", {0}, {T1, "\x1b", "0", "2", "g", "3", "q", "2", "z"}, VIRC("\"g\": vi-fword", "\"q\": vi-bword", "\"z\": vi-eword")},
  {"vi-word-operator", {0}, {T3, "\x1b", "0", "d", "g", "d", "z"}, VIRC("\"g\": vi-fword", "\"z\": vi-eword")},
  {"vi-back-to-indent", {0}, {"  one two", "\x1b", "$", "g", "0", "g"}, VIRC("\"g\": vi-back-to-indent")},
  {"vi-yank-pop", {0}, {T3, "\x1b", "0", "d", "w", "d", "w", "p", "g", "g"}, VIRC("\"g\": vi-yank-pop")},
  {"vi-yank-pop-not-after-put", {0}, {T3, "\x1b", "0", "d", "w", "g", "x"}, VIRC("\"g\": vi-yank-pop")},
  {"vi-overstrike", {0}, {"abc", "\x1b", "0", "i", "#", "#"}, .rc = {"set editing-mode vi", "\"#\": vi-overstrike"}},
  {"vi-R-rubout", {0}, {"abcdef", "\x1b", "0", "l", "R", "X", "Y", "\x7f", "\x7f", "\x7f", "\x7f", "Z"}, VI},
  {"vi-R-rubout-past-end", {0}, {"ab", "\x1b", "0", "R", "X", "Y", "Z", "\x7f", "\x7f", "\x7f"}, VI},
  {"vi-overstrike-delete", {0}, {"abc", "\x1b", "0", "R", "XY", "%", "%", "%"}, .rc = {"set editing-mode vi", "\"%\": vi-overstrike-delete"}},
  {"vi-overstrike-delete-insert", {0}, {"abc", "%", "%"}, .rc = {"set editing-mode vi", "\"%\": vi-overstrike-delete"}},
  {"vi-set-mark", {0}, {T1, "\x1b", "0", "w", "w", "m", "a", "0", "`", "a"}, VI},
  {"vi-goto-mark-unset", {0}, {T1, "\x1b", "`", "b", "x"}, VI},
  {"vi-goto-mark-backquote", {0}, {T1, "\x1b", "0", "w", "m", "a", "w", "`", "`"}, VI},
  {"vi-set-mark-bad", {0}, {T1, "\x1b", "m", "1", "x"}, VI},
  {"vi-fetch-history", H3, {"\x1b", "G"}, VI},
  {"vi-fetch-history-arg", H3, {"\x1b", "2", "G"}, VI},
  {"vi-fetch-history-arg-out", H3, {"abc", "\x1b", "9", "G"}, VI},
  {"vi-search", HS, {"\x1b", "/", "sel", "\r", "n", "n", "N"}, VI, .blocking = 1},
  {"vi-search-question", HS, {"\x1b", "k", "k", "k", "?", "sel", "\r", "n"}, VI, .blocking = 1},
  {"vi-search-esc", HS, {"abc", "\x1b", "/", "sel", "\x1b", "x", "\r"}, VI, .blocking = 1},
  {"vi-search-again-none", HS, {"\x1b", "n", "N", "x"}, VI, .blocking = 1},
  {"vi-search-empty", HS, {"\x1b", "/", "sel", "\r", "/", "\r"}, VI, .blocking = 1},
  {"vi-search-fail", HS, {"abc", "\x1b", "/", "zz", "\r", "x"}, VI, .blocking = 1},
  {"vi-search-rubout", HS, {"\x1b", "/", "sx", "\x7f", "how", "\r"}, VI, .blocking = 1},
  {"vi-yank-arg", {"ls one two", 0}, {"abc", "\x1b", "_", "Z"}, VI},
  {"vi-yank-arg-count", {"ls one two", 0}, {"abc", "\x1b", "1", "_", "Z"}, VI},
  {"vi-yank-arg-none", {0}, {"abc", "\x1b", "_", "x"}, VI},
  {"vi-tilde-expand", {0}, {"ls ~/x", "\x1b", "&", "Z"}, VI},
  {"vi-eof-maybe", {0}, {"abc", "\x04"}, VI},
  {"vi-eof-maybe-command", {0}, {"abc", "\x1b", "\x04"}, VI},
  {"vi-eof-maybe-command-empty", {0}, {"\x1b", "\x04"}, VI},
  {"vi-complete", {0}, {"se", "\x1b", "\\", "Z"}, VI, .words = W},
  {"vi-complete-star", {0}, {"se", "\x1b", "*", "Z"}, VI, .words = W},
  {"vi-complete-equal", {0}, {"se", "\x1b", "=", "x"}, VI, .words = W},
  {"vi-complete-one", {0}, {"sho", "\x1b", "\\", "Z"}, VI, .words = W},
  {"vi-complete-midword", {0}, {"sho x", "\x1b", "0", "l", "\\", "Z"}, VI, .words = W},
#undef W
#undef HS
#undef H3
#undef VIRC
#undef VI
#undef T3

  /* Commands that readline binds by default and rline added later. */
#define H3 {"first", "second", "third", 0}
#define HS {"select 1", "show tables", "select 2", 0}
  {"operate-and-get-next", H3, {"\x10", "\x10", "\x0f", "\x05", "\x0f", "\x05"}, .again = 1, .blocking = 1},
  {"operate-and-get-next-arg", H3, {"\x1b" "1", "\x0f", "\x05"}, .again = 1, .blocking = 1},
  {"operate-and-get-next-arg2", H3, {"\x1b" "2", "\x0f", "\x05"}, .again = 1, .blocking = 1},
  {"operate-and-get-next-new", H3, {"abc", "\x0f", "\x05"}, .again = 1, .blocking = 1},
  {"execute-named-command", {0}, {T1, "\x01", "\x1b" "x", "forward-word", "\r", "\x05"}, .blocking = 1},
  {"execute-named-command-arg", {0}, {T1, "\x01", "\x1b" "3", "\x1b" "x", "forward-word", "\r"}, .blocking = 1},
  {"execute-named-command-unknown", {0}, {T1, "\x1b" "x", "no-such-command", "\r", "x"}, .blocking = 1},
  {"execute-named-command-abort", {0}, {T1, "\x1b" "x", "forw", "\x07", "x"}, .blocking = 1},
  {"execute-named-command-rubout", {0}, {T1, "\x01", "\x1b" "x", "forward-wordx", "\x7f", "\r"}, .blocking = 1},
  {"execute-named-command-complete", {0}, {T1, "\x01", "\x1b" "x", "forward-w", "\t", "\r"}, .blocking = 1},
  {"execute-named-command-ambiguous", {0}, {T1, "\x01", "\x1b" "x", "forward-", "\t", "w", "\t", "\r"}, .blocking = 1},
  {"execute-named-command-rubout-empty", {0}, {T1, "\x1b" "x", "f", "\x7f", "\x7f", "x"}, .blocking = 1},
  {"execute-named-command-empty", {0}, {T1, "\x1b" "x", "\r", "x"}, .blocking = 1},
  {"nisearch-reverse", HS, {"\x1b" "p", "sel", "\r", "\x1b" "p", "\r"}, .blocking = 1},
  {"nisearch-forward", HS, {"\x1b" "<", "\x1b" "n", "sel", "\r"}, .blocking = 1},
  {"nisearch-abort", HS, {"abc", "\x1b" "p", "sel", "\x07", "x"}, .blocking = 1},
  {"nisearch-rubout", HS, {"\x1b" "p", "sx", "\x7f", "how", "\r"}, .blocking = 1},
  {"nisearch-fail", HS, {"abc", "\x1b" "p", "zz", "\r", "x"}, .blocking = 1},
  {"nisearch-rubout-empty", HS, {"abc", "\x1b" "p", "\x7f", "x"}, .blocking = 1},
  {"nisearch-again", HS, {"\x1b" "p", "sel", "\r", "\x18" "a", "\x18" "a", "\x18" "b"},
   {"\\C-xa", "non-incremental-reverse-search-history-again", "\\C-xb", "non-incremental-forward-search-history-again"}},
  {"nisearch-ctrl-u", HS, {"\x1b" "p", "sel", "\x15", "show", "\r"}, .blocking = 1},
  {"nisearch-ctrl-w", HS, {"\x1b" "p", "show tab", "\x17", "x", "\r"}, .blocking = 1},
  {"nisearch-ctrl-v", HS, {"abc", "\x1b" "p", "s", "\x16", "\x01", "\x02", "\r"}, .blocking = 1},
  {"nisearch-ctrl-v-ctrl-u", HS, {"abc", "\x1b" "p", "s", "\x16", "\x15", "x", "\r"}, .blocking = 1},
  {"nisearch-ctrl-v-enter", HS, {"abc", "\x1b" "p", "s", "\x16", "\r", "x", "\r"}, .blocking = 1},
  {"nisearch-ctrl-v-ctrl-g", HS, {"abc", "\x1b" "p", "s", "\x16", "\x07", "x", "\r"}, .blocking = 1},
  {"nisearch-esc", HS, {"\x1b" "p", "sel", "\x1b", "x", "\r"}, .blocking = 1},
  {"nisearch-again-first", HS, {"\x18" "a", "x"}, {"\\C-xa", "non-incremental-reverse-search-history-again"}, .blocking = 1},

  /* Commands that readline has and binds to no key. */
  {"fetch-history", H3, {"\x18" "g"}, {"\\C-xg", "fetch-history"}},
  {"fetch-history-arg", H3, {"\x1b" "2", "\x18" "g"}, {"\\C-xg", "fetch-history"}},
  {"fetch-history-arg-out", H3, {"abc", "\x1b" "9", "\x18" "g"}, {"\\C-xg", "fetch-history"}},
  {"fetch-history-arg-zero", H3, {"abc", "\x1b" "0", "\x18" "g"}, {"\\C-xg", "fetch-history"}},
#define UA {"\\C-xu", "universal-argument"}
  {"universal-argument", {0}, {T1, "\x01", "\x18" "u", "\x06"}, UA},
  {"universal-argument-twice", {0}, {T1, "\x01", "\x18" "u", "\x18" "u", "\x06"}, UA},
  {"universal-argument-digits", {0}, {T1, "\x01", "\x18" "u", "1", "2", "\x06"}, UA},
  {"universal-argument-ends", {0}, {"ab", "\x18" "u", "1", "2", "\x18" "u", "5"}, UA},
  {"universal-argument-minus", {0}, {T1, "\x18" "u", "-", "\x06"}, UA},
  {"universal-argument-insert", {0}, {"ab", "\x18" "u", "x"}, UA},
#undef UA
#define W {"select", "selection", "set", "show", "shutdown", "drop", 0}
  {"delete-char-or-list", {0}, {"sel x", "\x01", "\x18" "d"}, {"\\C-xd", "delete-char-or-list"}, .words = W},
  {"delete-char-or-list-end", {0}, {"se", "\x18" "d", "\x18" "d"}, {"\\C-xd", "delete-char-or-list"}, .words = W},
  {"delete-char-or-list-middle", {0}, {"sel x", "\x02", "\x02", "\x18" "d"}, {"\\C-xd", "delete-char-or-list"}, .words = W},
  {"delete-char-or-list-empty", {0}, {"\x18" "d", "x"}, {"\\C-xd", "delete-char-or-list"}, .words = W},
  {"old-menu-complete", {0}, {"se", "\x18" "m", "\x18" "m", "\x18" "m", "\x18" "m", "\x18" "m"},
   {"\\C-xm", "old-menu-complete"}, .words = W},
  {"old-menu-complete-one", {0}, {"sho", "\x18" "m", "\x18" "m"}, {"\\C-xm", "old-menu-complete"}, .words = W},
  {"old-menu-complete-then-type", {0}, {"se", "\x18" "m", "\x18" "m", "x"}, {"\\C-xm", "old-menu-complete"}, .words = W},
  {"export-completions", {0}, {"se", "\x18" "q", "x"}, {"\\C-xq", "export-completions"}, .words = W, .out = 1},
  {"export-completions-one", {0}, {"sho", "\x18" "q"}, {"\\C-xq", "export-completions"}, .words = W, .out = 1},
  {"export-completions-none", {0}, {"zz", "\x18" "q"}, {"\\C-xq", "export-completions"}, .words = W, .out = 1},
  {"export-completions-midline", {0}, {"drop sh x", "\x02", "\x02", "\x18" "q"}, {"\\C-xq", "export-completions"}, .words = W, .out = 1},
#undef W
  {"skip-csi-sequence", {0}, {"ab", "\x1b[1;5P", "\x1b[D", "x"}, {"\\e[", "skip-csi-sequence"}},
  {"arrow-key-prefix", {0}, {T1, "\x18" "a", "D", "\x18" "a", "D", "\x18" "a", "C", "\x18" "a", "x"}, {"\\C-xa", "arrow-key-prefix"}, .blocking = 1},
  {"arrow-key-prefix-history", H3, {"\x18" "a", "A", "\x18" "a", "A", "\x18" "a", "B"}, {"\\C-xa", "arrow-key-prefix"}, .blocking = 1},
  {"tty-status", {0}, {"ab", "\x18" "t", "x"}, {"\\C-xt", "tty-status"}, .out = 1},
#define L200 "0123456789abcdefghij0123456789abcdefghij0123456789abcdefghij0123456789abcdefghij" \
             "0123456789abcdefghij0123456789abcdefghij0123456789abcdefghij0123456789abcdefghij" \
             "0123456789abcdefghij0123456789abcdefghij"
#define SL {"\\C-xn", "next-screen-line", "\\C-xp", "previous-screen-line"}
  {"screen-line-next", {0}, {L200, "\x01", "\x06", "\x06", "\x06", "\x18" "n", "\x18" "n", "\x18" "n", "\x18" "p"}, SL},
  {"screen-line-previous", {0}, {L200, "\x18" "p", "\x18" "p", "\x18" "p", "\x18" "n", "\x18" "n"}, SL},
  {"screen-line-short", {0}, {"abc", "\x02", "\x18" "n", "\x18" "p"}, SL},
#undef SL
#undef L200
  {"print-last-kbd-macro", {0}, {"\x18" "(", "a", "b", "\x02", "\x18" ")", "\x18" "q"}, {"\\C-xq", "print-last-kbd-macro"}, .out = 1},
  {"print-last-kbd-macro-none", {0}, {"\x18" "q", "x"}, {"\\C-xq", "print-last-kbd-macro"}, .out = 1},
  {"dump-macros", {0}, {"\x18" "q", "\x1b" "1", "\x18" "q"}, {"\\C-xq", "dump-macros"},
   .rc = {"\"\\C-xy\": \"hello\"", "\"\\ez\": \"a\\tb\""}, .out = 1},
  {"dump-variables", {0}, {"\x18" "q", "\x1b" "1", "\x18" "q"}, {"\\C-xq", "dump-variables"}, .out = 1},
  {"dump-functions", {0}, {"\x18" "q", "\x1b" "1", "\x18" "q"}, {"\\C-xq", "dump-functions"}, .out = 1},
#undef HS
#undef H3

  /* Ending the line. */
  {"accept-line", {0}, {"abc", "\r"}},
  {"eof-on-empty", {0}, {"\x04"}},
  {"insert-comment", {0}, {"abc", "\x1b" "#"}},
};

/* print_binds prints every key sequence that the default keymap binds to a
   command, one to a line: the keymap, the command and the sequence in the
   escaped form of an inputrc file. */
static void print_binds(const char *keymap) {
  Keymap map = rl_get_keymap_by_name(keymap);
  const char **names = rl_funmap_names();
  for (int i = 0; names != NULL && names[i] != NULL; i++) {
    rl_command_func_t *fn = rl_named_function(names[i]);
    char **seqs = rl_invoking_keyseqs_in_map(fn, map);
    for (int j = 0; seqs != NULL && seqs[j] != NULL; j++) {
      printf("%s %s %s\n", keymap, names[i], seqs[j]);
      free(seqs[j]);
    }
    free(seqs);
  }
  free(names);
}

/* The key of a blocking case that readline is reading, and the last one
   whose state was printed. */
static int block_key, block_printed;

/* key_len is how many bytes a key of a case has. A key of one NUL byte is
   set-mark on C-@, which strlen cannot see. */
static size_t key_len(const char *key) {
  size_t n = strlen(key);
  return n == 0 ? 1 : n;
}

/* print_block prints the state after key k of the running blocking case. */
static void print_block(int k) {
  const char *key = running->keys[k];
  size_t n = key_len(key);
  if (k == 0 && n > 2) {
    print_state(running->name, "", 0);
  } else {
    print_state(running->name, key, n);
  }
  if (running->out) print_out(running->name);
  block_printed = k;
}

/* getc_block gives readline() the bytes of the keys of a blocking case.
   When readline asks for a byte after the last one of a key, that key is
   done, and its state is printed. After the last key it gives EOF. */
static int getc_block(FILE *f) {
  (void)f;
  while (feed_pos >= feed_len) {
    if (block_key >= 0 && block_printed < block_key) print_block(block_key);
    if (running->keys[block_key + 1] == NULL) return EOF;
    block_key++;
    feed = running->keys[block_key];
    feed_len = key_len(feed);
    feed_pos = 0;
  }
  return (unsigned char)feed[feed_pos++];
}

/* run_blocking runs the keys of the running case through readline(). The
   key that makes readline hand a line back is printed when it returns. */
static void run_blocking(void) {
  rl_getc_function = getc_block;
  block_key = -1;
  block_printed = -1;
  feed_len = feed_pos = 0;
  for (;;) {
    done = 0;
    char *line = readline("");
    if (block_key >= 0 && block_printed < block_key) {
      done = 1;
      print_block(block_key);
    }
    free(line);
    if (line == NULL || !running->again || running->keys[block_key + 1] == NULL) return;
  }
}

int main(int argc, char **argv) {
  setlocale(LC_ALL, "C.UTF-8");
  /* tilde-expand reads HOME, so it is fixed for the recording. readline
     binds the Insert, Delete, PageUp and PageDown keys from the terminal's
     capabilities, so TERM is fixed too, as xterm, rather than taken from
     the shell the probe runs in. */
  setenv("HOME", "/home/probe", 1);
  setenv("TERM", "xterm", 1);
  setenv("INPUTRC", "/dev/null", 1);
  rl_readline_name = "rline-probe";
  rl_getc_function = getc_feed;
  /* readline asks whether more input is waiting on rl_instream, to insert
     typed ahead characters together. Its input is fed through getc, so its
     stream is a pipe that never has anything waiting: each key is then one
     key, as when a person types it. */
  int never[2];
  if (pipe(never) != 0) return 1;
  rl_instream = fdopen(never[0], "r");
  rl_prep_term_function = prep;
  rl_deprep_term_function = deprep;
  rl_outstream = fopen("/dev/null", "w");
  rl_catch_signals = 0;
  rl_catch_sigwinch = 0;
  rl_set_screen_size(24, 80);
  using_history();
  /* An Esc that starts a key sequence waits for the next key however long it
     takes. With a timeout, the probe would read its own empty input as the
     user pausing, and an Esc prefix would end early. */
  rl_variable_bind("keyseq-timeout", "0");

  printf("readline %s\n", rl_library_version);
  /* With the argument binds, print the default keymaps instead of the
     cases. */
  if (argc > 1 && strcmp(argv[1], "binds") == 0) {
    rl_initialize();
    /* Every command readline has, bound or not, as "command <name>". */
    const char **names = rl_funmap_names();
    for (int i = 0; names != NULL && names[i] != NULL; i++) printf("command %s\n", names[i]);
    free(names);
    print_binds("emacs");
    print_binds("vi-insert");
    print_binds("vi-command");
    return 0;
  }
  size_t ncases = sizeof(cases) / sizeof(cases[0]);
  fflush(stdout);
  for (size_t c = 0; c < ncases; c++) {
    pid_t pid = fork();
    if (pid < 0) return 1;
    if (pid > 0) {
      int status;
      if (waitpid(pid, &status, 0) < 0 || !WIFEXITED(status) || WEXITSTATUS(status) != 0) return 1;
      continue;
    }
    if (cases[c].out) {
      rl_outstream = tmpfile();
      if (rl_outstream == NULL) return 1;
      written = 0;
      capture = 1;
    }
    reset();
    running = &cases[c];
    for (int r = 0; cases[c].rc[r] != NULL; r++) {
      printf("rc %s ", cases[c].name);
      print_bytes(cases[c].rc[r]);
      printf("\n");
      char *line = strdup(cases[c].rc[r]);
      if (rl_parse_and_bind(line) != 0) return 1;
      free(line);
    }
    if (cases[c].words[0] != NULL) {
      rl_completion_entry_function = complete_word;
      rl_completion_display_matches_hook = show_list;
      printf("words %s", cases[c].name);
      for (int w = 0; cases[c].words[w] != NULL; w++) {
        printf(" ");
        print_bytes(cases[c].words[w]);
      }
      printf("\n");
    }
    for (int h = 0; cases[c].history[h] != NULL; h++) {
      add_history(cases[c].history[h]);
      printf("history %s ", cases[c].name);
      print_bytes(cases[c].history[h]);
      printf("\n");
    }
    /* add_history leaves the position where it was. A program puts it after
       the newest entry, so that the previous entry is the newest one. */
    using_history();
    for (int b = 0; cases[c].binds[b] != NULL; b += 2) {
      const char *seq = cases[c].binds[b], *fn = cases[c].binds[b + 1];
      if (rl_bind_keyseq(seq, rl_named_function(fn)) != 0) return 1;
      printf("bind %s %s ", cases[c].name, fn);
      print_bytes(seq);
      printf("\n");
    }
    if (cases[c].blocking) {
      rl_callback_handler_remove();
      run_blocking();
      fflush(stdout);
      _exit(0);
    }
    for (int k = 0; cases[c].keys[k] != NULL; k++) {
      const char *key = cases[c].keys[k];
      size_t n = strlen(key);
      /* A key of one NUL byte is set-mark on C-@. strlen cannot see it. */
      if (n == 0) n = 1;
      /* The first key of most cases is the text. It is fed whole, so that a
         character of several bytes arrives whole, and the step is printed
         with - as its key, because it is not one key. */
      if (k == 0 && n > 2) {
        feed_key(key, n);
        print_state(cases[c].name, "", 0);
        if (cases[c].out) print_out(cases[c].name);
        continue;
      }
      /* After a line is handed back, readline is ready for the next one. */
      if (done && cases[c].again) done = 0;
      step(cases[c].name, key, n);
      if (cases[c].out) print_out(cases[c].name);
    }
    rl_callback_handler_remove();
    fflush(stdout);
    _exit(0);
  }
  return 0;
}
