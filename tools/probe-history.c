/* Print what isocline's history.c and undo.c do, so that the Go port can be
   checked against them.

   This file includes isocline.c, which is the unity build of the whole
   library. The functions of both modules are static there, so including the
   source is the only way to reach them.

   The file format is the part worth pinning. An entry is escaped on the way
   out and read back on the way in, and the two have to agree. Every case here
   goes through a temporary file, which is what the real code writes to.

   The output is a golden file. Every line is one case and its result. */
/* These must come before any system header. isocline.c sets them too, but by
   then the C library headers have already fixed which interfaces they expose,
   and completers.c needs lstat and the S_IF* constants. */
#define _XOPEN_SOURCE 700
#define _DEFAULT_SOURCE

#include "isocline.c"

#include <stdio.h>
#include <stdint.h>
#include <stdlib.h>

static alloc_t probe_mem = { &malloc, &realloc, &free };

static void print_bytes(const uint8_t* b, ssize_t n) {
  if (n <= 0) { printf("-"); return; }
  for (ssize_t i = 0; i < n; i++) printf("%02x", b[i]);
}

static void print_str(const char* s) {
  if (s == NULL) { printf("!"); return; }
  print_bytes((const uint8_t*)s, (ssize_t)strlen(s));
}

/* ------------------------------------------------------------------------
   The entry corpus.

   These cover every byte that the escaping treats differently: the ones with
   their own escape, the comment marker, the control bytes, and the bytes
   above 0x7f that go out as hexadecimal.
   ------------------------------------------------------------------------ */
static const char* entries[] = {
  "", "a", "hello", "hello world", " leading", "trailing ",
  "with\nnewline", "with\ttab", "with\\backslash", "with\rcarriage",
  "#comment", "a#b", "#", "##",
  "\x01\x02", "\x1b[31m", "\x7f", "\xff", "\x80", "\xc3\xa9",
  "\xe6\x97\xa5\xe6\x9c\xac", "\xf0\x9f\x98\x80",
  "\\n", "\\x41", "\\", "a\\", "\\\\",
  "line1\nline2\nline3", "tab\there\tand\there",
  "~", "}", "!", "0123456789",
};
#define NENTRIES ((int)(sizeof(entries)/sizeof(entries[0])))

/* Lines as they might appear in a history file, including ones that are not
   valid and that stop the read. */
static const char* lines[] = {
  "hello", "", "   ", "#comment", "# ", "#",
  "with\\nnewline", "with\\ttab", "with\\\\backslash", "with\\rcarriage",
  "\\x41", "\\x4a", "\\x4A", "\\xff", "\\x00", "\\x0a",
  "\\xZZ", "\\x4", "\\x", "\\q", "\\", "a\\",
  "a\rb", "\ra", "a\r",
  "plain\\n\\t\\\\done",
  "\\x41\\x42\\x43",
  "  spaced  ",
};
#define NLINES ((int)(sizeof(lines)/sizeof(lines[0])))

/* ------------------------------------------------------------------------
   Escaping one entry

   history_write_entry writes to a FILE, so the case writes to a temporary
   file and reads the bytes back.
   ------------------------------------------------------------------------ */
static void write_entry_case(const char* entry) {
  FILE* f = tmpfile();
  if (f == NULL) return;
  stringbuf_t* sbuf = sbuf_new(&probe_mem);
  history_write_entry(entry, f, sbuf);
  sbuf_free(sbuf);
  fflush(f);
  rewind(f);
  uint8_t buf[1024];
  size_t n = fread(buf, 1, sizeof(buf), f);
  fclose(f);
  printf("wentry ");
  print_str(entry);
  printf(" ");
  print_bytes(buf, (ssize_t)n);
  printf("\n");
}

/* Build an empty history with room for max entries. history_load_from is the
   only thing that allocates the array, and it reads a file as well, so this
   hands it a name that is not there. */
static history_t* history_of(ssize_t max, bool dups) {
  history_t* h = history_new(&probe_mem);
  history_enable_duplicates(h, dups);
  history_load_from(h, "/nonexistent/rline-probe-history", (long)max);
  return h;
}

static void print_history(const history_t* h) {
  printf(" %zd", history_count(h));
  for (ssize_t i = 0; i < history_count(h); i++) {
    printf(" ");
    print_str(history_get(h, i));
  }
}

/* Print a list of inputs, led by how many there are, so that a recorded line
   carries everything it took as well as everything it gave. */
static void print_inputs(const char* const* in, int n) {
  printf(" %d", n);
  for (int i = 0; i < n; i++) {
    printf(" ");
    print_str(in[i]);
  }
}

/* ------------------------------------------------------------------------
   Reading one line back
   ------------------------------------------------------------------------ */
static void read_entry_case(const char* line) {
  FILE* f = tmpfile();
  if (f == NULL) return;
  fputs(line, f);
  fputc('\n', f);
  fflush(f);
  rewind(f);
  history_t* h = history_of(8, true);
  stringbuf_t* sbuf = sbuf_new(&probe_mem);
  bool ok = history_read_entry(h, f, sbuf);
  sbuf_free(sbuf);
  fclose(f);
  printf("rentry ");
  print_str(line);
  printf(" %d", ok ? 1 : 0);
  print_history(h);
  printf("\n");
  history_free(h);
}

/* ------------------------------------------------------------------------
   A whole file, written and read back
   ------------------------------------------------------------------------ */
static void round_trip_case(int first, int n, ssize_t max, bool dups) {
  char fname[256];
  snprintf(fname, sizeof(fname), "/tmp/rline-probe-history-%d-%d-%zd-%d.txt",
           first, n, max, dups ? 1 : 0);
  remove(fname);

  const char* pushed[16];
  for (int i = 0; i < n && i < 16; i++) {
    pushed[i] = entries[(first + i) % NENTRIES];
  }

  history_t* h = history_new(&probe_mem);
  history_enable_duplicates(h, dups);
  history_load_from(h, fname, (long)max);
  for (int i = 0; i < n; i++) {
    history_push(h, pushed[i]);
  }
  history_save(h);
  history_free(h);

  /* Read the file back as bytes, then load it into a fresh history. */
  FILE* f = fopen(fname, "r");
  uint8_t buf[4096];
  size_t len = 0;
  if (f != NULL) {
    len = fread(buf, 1, sizeof(buf), f);
    fclose(f);
  }
  history_t* h2 = history_new(&probe_mem);
  history_enable_duplicates(h2, dups);
  history_load_from(h2, fname, (long)max);

  printf("file %zd %d", max, dups ? 1 : 0);
  print_inputs(pushed, n);
  printf(" ");
  print_bytes(buf, (ssize_t)len);
  print_history(h2);
  printf("\n");
  history_free(h2);
  remove(fname);
}

/* ------------------------------------------------------------------------
   Pushing, updating and removing
   ------------------------------------------------------------------------ */
static void push_case(ssize_t max, bool dups, const char* const* push, int n) {
  history_t* h = history_of(max, dups);
  printf("push %zd %d", max, dups ? 1 : 0);
  print_inputs(push, n);
  for (int i = 0; i < n; i++) {
    printf(" %d", history_push(h, push[i]) ? 1 : 0);
  }
  print_history(h);
  printf("\n");
  history_free(h);
}

static void update_case(ssize_t max, bool dups, const char* const* push, int n, const char* with) {
  history_t* h = history_of(max, dups);
  for (int i = 0; i < n; i++) history_push(h, push[i]);
  bool ok = history_update(h, with);
  printf("update %zd %d", max, dups ? 1 : 0);
  print_inputs(push, n);
  printf(" ");
  print_str(with);
  printf(" %d", ok ? 1 : 0);
  print_history(h);
  printf("\n");
  history_free(h);
}

/* get past both ends, to pin what is out of range. */
static void get_case(ssize_t max, const char* const* push, int n) {
  history_t* h = history_of(max, true);
  for (int i = 0; i < n; i++) history_push(h, push[i]);
  printf("get %zd", max);
  print_inputs(push, n);
  for (ssize_t i = -1; i <= (ssize_t)n + 1; i++) {
    printf(" ");
    print_str(history_get(h, i));
  }
  printf("\n");
  history_free(h);
}

static void remove_case(ssize_t max, const char* const* push, int n, int times) {
  history_t* h = history_of(max, true);
  for (int i = 0; i < n; i++) history_push(h, push[i]);
  for (int i = 0; i < times; i++) history_remove_last(h);
  printf("remove %zd %d", max, times);
  print_inputs(push, n);
  print_history(h);
  printf("\n");
  history_free(h);
}

/* ------------------------------------------------------------------------
   Searching

   history_search asks history_get for the entry at each index and hands the
   answer straight to strstr. history_get answers NULL outside the range, so a
   starting index at or past the count reads through a null pointer when the
   search runs forwards. That is undefined, so the corpus keeps the starting
   index inside the range.
   ------------------------------------------------------------------------ */
static void search_case(const char* const* push, int n, ssize_t from,
                        const char* needle, bool backward) {
  history_t* h = history_of(16, true);
  for (int i = 0; i < n; i++) history_push(h, push[i]);
  ssize_t hidx = -111, hpos = -222;
  bool ok = false;
  if (from >= 0 && from < history_count(h)) {
    ok = history_search(h, from, needle, backward, &hidx, &hpos);
  }
  printf("search");
  print_inputs(push, n);
  printf(" %zd ", from);
  print_str(needle);
  printf(" %d %d %zd %zd\n", backward ? 1 : 0, ok ? 1 : 0, hidx, hpos);
  history_free(h);
}

/* ------------------------------------------------------------------------
   The undo stack
   ------------------------------------------------------------------------ */
static void editstate_case(const char* const* inputs, int n, int restores) {
  editstate_t* es = NULL;
  editstate_init(&es);
  for (int i = 0; i < n; i++) {
    editstate_capture(&probe_mem, &es, inputs[i], (ssize_t)i);
  }
  printf("undo");
  print_inputs(inputs, n);
  printf(" %d", restores);
  for (int i = 0; i < restores; i++) {
    const char* input = NULL;
    ssize_t pos = -1;
    bool ok = editstate_restore(&probe_mem, &es, &input, &pos);
    printf(" %d:", ok ? 1 : 0);
    print_str(ok ? input : NULL);
    printf(":%zd", pos);
    if (ok) mem_free(&probe_mem, input);
  }
  printf("\n");
  editstate_done(&probe_mem, &es);
}

int main(void) {
  for (int i = 0; i < NENTRIES; i++) write_entry_case(entries[i]);
  for (int i = 0; i < NLINES; i++) read_entry_case(lines[i]);

  /* Every byte on its own, so that the escaping is pinned byte by byte. */
  for (int b = 1; b < 256; b++) {
    char one[2] = { (char)b, 0 };
    write_entry_case(one);
  }

  static const char* dup[] = { "a", "b", "a", "a", "b", "c", "a" };
  static const char* few[] = { "one", "two", "three", "four", "five" };
  static const char* same[] = { "x", "x", "x" };
  static const char* empty[] = { "", "a", "" };

  push_case(4, false, dup, 7);
  push_case(4, true, dup, 7);
  push_case(3, false, few, 5);
  push_case(3, true, few, 5);
  push_case(1, false, few, 5);
  push_case(1, true, few, 5);
  push_case(0, false, few, 5);
  push_case(0, true, few, 5);
  push_case(8, false, same, 3);
  push_case(8, true, same, 3);
  push_case(8, false, empty, 3);

  update_case(4, false, few, 3, "updated");
  update_case(4, true, few, 3, "updated");
  update_case(4, false, few, 0, "updated");
  update_case(4, false, few, 3, "");
  update_case(0, false, few, 3, "updated");

  get_case(8, few, 5);
  get_case(8, few, 0);

  for (int times = 0; times <= 6; times++) remove_case(8, few, 5, times);

  static const char* sr[] = { "alpha", "beta", "gamma", "alphabet", "beta" };
  static const char* needles[] = { "a", "beta", "z", "", "alpha", "ta" };
  for (int nd = 0; nd < (int)(sizeof(needles)/sizeof(needles[0])); nd++) {
    for (ssize_t from = 0; from < 5; from++) {
      search_case(sr, 5, from, needles[nd], true);
      search_case(sr, 5, from, needles[nd], false);
    }
  }

  for (int first = 0; first < NENTRIES; first += 3) {
    round_trip_case(first, 5, 8, true);
  }
  round_trip_case(0, 3, 2, true);
  round_trip_case(0, 0, 4, true);

  static const char* undo1[] = { "a", "ab", "abc" };
  static const char* undo2[] = { "", "x" };
  for (int r = 0; r <= 4; r++) editstate_case(undo1, 3, r);
  for (int r = 0; r <= 3; r++) editstate_case(undo2, 2, r);
  editstate_case(undo1, 0, 1);

  return 0;
}
