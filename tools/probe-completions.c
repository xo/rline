/* Print what isocline's completions.c and completers.c do, so that the Go
   port can be checked against them.

   This file includes isocline.c, which is the unity build of the whole
   library. The functions of both modules are static there, so including the
   source is the only way to reach them.

   Word completion runs through the real path: a completer is set on the
   completions, completions_generate calls it, and the completer calls
   ic_complete_word or ic_complete_qword_ex, which wrap the function that
   records what it was handed. ic_env_t is a plain structure, so the probe
   fills one in rather than starting a terminal.

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

static void print_inputs(const char* const* in, int n) {
  printf(" %d", n);
  for (int i = 0; i < n; i++) { printf(" "); print_str(in[i]); }
}

/* ------------------------------------------------------------------------
   The environment

   Only mem and completions are ever read on these paths, so the rest stays
   zero.
   ------------------------------------------------------------------------ */
static ic_env_t probe_env;

static completions_t* fresh_completions(void) {
  memset(&probe_env, 0, sizeof(probe_env));
  probe_env.mem = &probe_mem;
  completions_t* cms = completions_new(&probe_mem);
  probe_env.completions = cms;
  return cms;
}

/* Print every completion, with the two deletion counts that say how much of
   the line it replaces. */
static void print_completions(completions_t* cms) {
  printf(" %zd", completions_count(cms));
  for (ssize_t i = 0; i < completions_count(cms); i++) {
    const char* help = NULL;
    const char* display = completions_get_display(cms, i, &help);
    printf(" ");
    print_str(display);
    printf(":");
    print_str(help);
    const char* hhelp = NULL;
    const char* hint = completions_get_hint(cms, i, &hhelp);
    printf(":");
    print_str(hint);
  }
}

/* ------------------------------------------------------------------------
   Adding

   completions_add refuses once completer_max runs out, and it drops a
   replacement it already holds.
   ------------------------------------------------------------------------ */
static const char* const adds[] = { "alpha", "beta", "alpha", "Alpha", "", "gamma" };
#define NADDS ((int)(sizeof(adds)/sizeof(adds[0])))

static void add_case(ssize_t max, int n) {
  completions_t* cms = fresh_completions();
  cms->completer_max = max;
  printf("add %zd", max);
  print_inputs(adds, n);
  for (int i = 0; i < n; i++) {
    printf(" %d", completions_add(cms, adds[i], NULL, NULL, 0, 0) ? 1 : 0);
  }
  print_completions(cms);
  printf("\n");
  completions_free(cms);
}

/* display and help are kept as given, and an absent display falls back to the
   replacement. The case records what an empty string does as well as what a
   null pointer does, because Go has no null string. */
static void display_case(const char* repl, const char* display, const char* help) {
  completions_t* cms = fresh_completions();
  cms->completer_max = 10;
  completions_add(cms, repl, display, help, 0, 0);
  const char* gothelp = NULL;
  const char* got = completions_get_display(cms, 0, &gothelp);
  printf("display ");
  print_str(repl); printf(" "); print_str(display); printf(" "); print_str(help);
  printf(" "); print_str(got); printf(" "); print_str(gothelp);
  printf("\n");
  completions_free(cms);
}

/* The hint is what is left of the replacement after the part that is deleted,
   and there is none when that lands inside a character. */
static void hint_case(const char* repl, ssize_t before) {
  completions_t* cms = fresh_completions();
  cms->completer_max = 10;
  completions_add(cms, repl, NULL, "help", before, 0);
  const char* help = NULL;
  const char* hint = completions_get_hint(cms, 0, &help);
  printf("hint ");
  print_str(repl);
  printf(" %zd ", before);
  print_str(hint); printf(" "); print_str(help);
  printf("\n");
  completions_free(cms);
}

/* ------------------------------------------------------------------------
   Applying one completion to a line
   ------------------------------------------------------------------------ */
static void apply_case(const char* line, ssize_t pos, const char* repl,
                       ssize_t before, ssize_t after) {
  completions_t* cms = fresh_completions();
  cms->completer_max = 10;
  completions_add(cms, repl, NULL, NULL, before, after);
  stringbuf_t* sbuf = sbuf_new(&probe_mem);
  sbuf_append(sbuf, line);
  ssize_t res = completions_apply(cms, 0, sbuf, pos);
  printf("apply ");
  print_str(line);
  printf(" %zd ", pos);
  print_str(repl);
  printf(" %zd %zd %zd ", before, after, res);
  print_bytes((const uint8_t*)sbuf_string(sbuf), sbuf_len(sbuf));
  printf("\n");
  sbuf_free(sbuf);
  completions_free(cms);
}

/* Applying at an index that names nothing. */
static void apply_missing_case(ssize_t index) {
  completions_t* cms = fresh_completions();
  stringbuf_t* sbuf = sbuf_new(&probe_mem);
  sbuf_append(sbuf, "line");
  ssize_t res = completions_apply(cms, index, sbuf, 2);
  printf("applymissing %zd %zd ", index, res);
  print_bytes((const uint8_t*)sbuf_string(sbuf), sbuf_len(sbuf));
  printf("\n");
  sbuf_free(sbuf);
  completions_free(cms);
}

/* ------------------------------------------------------------------------
   Sorting

   The order is the one ic_stricmp gives, which compares by length first and
   then byte by byte with the letters folded. Entries that compare equal are
   left wherever the sort puts them, which is not defined, so the corpus keeps
   them apart.
   ------------------------------------------------------------------------ */
static void sort_case(const char* const* in, int n) {
  completions_t* cms = fresh_completions();
  cms->completer_max = 100;
  for (int i = 0; i < n; i++) completions_add(cms, in[i], NULL, NULL, 0, 0);
  completions_sort(cms);
  printf("sort");
  print_inputs(in, n);
  printf(" %zd", completions_count(cms));
  for (ssize_t i = 0; i < completions_count(cms); i++) {
    printf(" ");
    print_str(completions_get_display(cms, i, NULL));
  }
  printf("\n");
  completions_free(cms);
}

/* ------------------------------------------------------------------------
   The longest shared start of every completion
   ------------------------------------------------------------------------ */
/* The same, but with each entry taking away a different amount of the line.
   The shared start is only safe to fill in when every entry deletes the same
   number of bytes before the cursor, and prefix_case cannot show that,
   because it gives every entry the same count. */
static void prefix_mixed_case(const char* line, ssize_t pos,
                              const char* const* in, const ssize_t* befores, int n) {
  completions_t* cms = fresh_completions();
  cms->completer_max = 100;
  for (int i = 0; i < n; i++) completions_add(cms, in[i], NULL, NULL, befores[i], 0);
  stringbuf_t* sbuf = sbuf_new(&probe_mem);
  sbuf_append(sbuf, line);
  ssize_t res = completions_apply_longest_prefix(cms, sbuf, pos);
  printf("prefixmixed ");
  print_str(line);
  printf(" %zd", pos);
  print_inputs(in, n);
  printf(" %d", n);
  for (int i = 0; i < n; i++) printf(" %zd", befores[i]);
  printf(" %zd ", res);
  print_bytes((const uint8_t*)sbuf_string(sbuf), sbuf_len(sbuf));
  printf(" %zd", completions_count(cms));
  for (ssize_t i = 0; i < completions_count(cms); i++) {
    printf(" %zd", cms->elems[i].delete_before);
  }
  printf("\n");
  sbuf_free(sbuf);
  completions_free(cms);
}


static void prefix_case(const char* line, ssize_t pos, const char* const* in, int n, ssize_t before) {
  completions_t* cms = fresh_completions();
  cms->completer_max = 100;
  for (int i = 0; i < n; i++) completions_add(cms, in[i], NULL, NULL, before, 0);
  stringbuf_t* sbuf = sbuf_new(&probe_mem);
  sbuf_append(sbuf, line);
  ssize_t res = completions_apply_longest_prefix(cms, sbuf, pos);
  printf("prefix ");
  print_str(line);
  printf(" %zd %zd", pos, before);
  print_inputs(in, n);
  printf(" %zd ", res);
  print_bytes((const uint8_t*)sbuf_string(sbuf), sbuf_len(sbuf));
  /* The deletion counts are rewritten when a prefix is applied. */
  printf(" %zd", completions_count(cms));
  for (ssize_t i = 0; i < completions_count(cms); i++) {
    printf(" %zd", cms->elems[i].delete_before);
  }
  printf("\n");
  sbuf_free(sbuf);
  completions_free(cms);
}

/* ------------------------------------------------------------------------
   Word completion

   The recording completer writes down the word it was handed, which is the
   thing word and quoted word completion work out, and then adds a fixed set
   of replacements so that the deletion counts can be read off.
   ------------------------------------------------------------------------ */
static char captured[512];
static const char* const* completer_adds;
static int completer_nadds;

static void recording_completer(ic_completion_env_t* cenv, const char* prefix) {
  snprintf(captured, sizeof(captured), "%s", (prefix == NULL ? "" : prefix));
  for (int i = 0; i < completer_nadds; i++) {
    ic_add_completion(cenv, completer_adds[i]);
  }
}

/* The character classes a case can name, in a fixed order. */
static ic_is_char_class_fun_t* const classes[] = {
  NULL, &ic_char_is_nonseparator, &ic_char_is_idletter, &ic_char_is_filename_letter
};
#define NCLASSES ((int)(sizeof(classes)/sizeof(classes[0])))

static int cur_class;
static char cur_escape;
static const char* cur_quotes;

static void word_completer(ic_completion_env_t* cenv, const char* prefix) {
  ic_complete_word(cenv, prefix, &recording_completer, classes[cur_class]);
}

static void qword_completer(ic_completion_env_t* cenv, const char* prefix) {
  ic_complete_qword_ex(cenv, prefix, &recording_completer, classes[cur_class],
                       cur_escape, cur_quotes);
}

/* Print the completions with their deletion counts, which is what word
   completion adjusts. */
static void print_completions_raw(completions_t* cms) {
  printf(" %zd", completions_count(cms));
  for (ssize_t i = 0; i < completions_count(cms); i++) {
    printf(" ");
    print_str(cms->elems[i].replacement);
    printf(":%zd:%zd", cms->elems[i].delete_before, cms->elems[i].delete_after);
  }
}

static void word_case(const char* input, ssize_t cursor, int cls,
                      const char* const* add, int nadd, bool quoted,
                      char escape, const char* quotes) {
  completions_t* cms = fresh_completions();
  cur_class = cls;
  cur_escape = escape;
  cur_quotes = quotes;
  completer_adds = add;
  completer_nadds = nadd;
  captured[0] = 0;
  completions_set_completer(cms, quoted ? &qword_completer : &word_completer, NULL);
  completions_generate(&probe_env, cms, input, cursor, 100);

  printf("%s ", quoted ? "qword" : "word");
  print_str(input);
  printf(" %zd %d", cursor, cls);
  if (quoted) {
    printf(" %02x ", (unsigned char)escape);
    print_str(quotes);
  }
  print_inputs(add, nadd);
  printf(" ");
  print_str(captured);
  print_completions_raw(cms);
  printf("\n");
  completions_free(cms);
}

int main(void) {
  for (ssize_t max = 0; max <= 4; max++) add_case(max, NADDS);
  add_case(100, NADDS);

  display_case("repl", NULL, NULL);
  display_case("repl", "", "");
  display_case("repl", "shown", "helpful");
  display_case("repl", NULL, "helpful");
  display_case("repl", "shown", NULL);

  static const char* hints[] = { "", "a", "hello", "\xe6\x97\xa5\xe6\x9c\xac", "\xc3\xa9x" };
  for (int i = 0; i < (int)(sizeof(hints)/sizeof(hints[0])); i++) {
    for (ssize_t before = -1; before <= 6; before++) hint_case(hints[i], before);
  }

  static const char* lines[] = { "", "a", "hello", "hello world", "\xe6\x97\xa5\xe6\x9c\xac" };
  static const char* repls[] = { "", "x", "hello", "help", "\xe6\x97\xa5" };
  for (int l = 0; l < (int)(sizeof(lines)/sizeof(lines[0])); l++) {
    ssize_t len = (ssize_t)strlen(lines[l]);
    for (ssize_t pos = 0; pos <= len; pos++) {
      for (int r = 0; r < (int)(sizeof(repls)/sizeof(repls[0])); r++) {
        for (ssize_t before = 0; before <= 2; before++) {
          for (ssize_t after = 0; after <= 2; after++) {
            apply_case(lines[l], pos, repls[r], before, after);
          }
        }
      }
    }
  }
  for (ssize_t i = -1; i <= 1; i++) apply_missing_case(i);

  static const char* s1[] = { "pear", "Apple", "banana", "fig", "date" };
  static const char* s2[] = { "b", "aa", "ccc", "dd" };
  static const char* s3[] = { "one" };
  sort_case(s1, 5);
  sort_case(s2, 4);
  sort_case(s3, 1);

  static const char* p1[] = { "prefix_one", "prefix_two", "prefix_three" };
  static const char* p2[] = { "same", "same_longer" };
  static const char* p3[] = { "alpha", "beta" };
  static const char* p4[] = { "only" };
  for (ssize_t before = 0; before <= 3; before++) {
    prefix_case("pre", 3, p1, 3, before);
    prefix_case("sa", 2, p2, 2, before);
    prefix_case("x", 1, p3, 2, before);
    prefix_case("on", 2, p4, 1, before);
  }

  /* Entries that do not agree on how much they take away. */
  {
    static const char* m1[] = { "alpha", "alpine" };
    static const ssize_t b_same[] = { 1, 1 };
    static const ssize_t b_diff[] = { 1, 2 };
    static const ssize_t b_first[] = { 2, 1 };
    static const char* m2[] = { "alpha", "alpine", "almond" };
    static const ssize_t b3_same[] = { 1, 1, 1 };
    static const ssize_t b3_last[] = { 1, 1, 0 };
    prefix_mixed_case("al", 2, m1, b_same, 2);
    prefix_mixed_case("al", 2, m1, b_diff, 2);
    prefix_mixed_case("al", 2, m1, b_first, 2);
    prefix_mixed_case("al", 2, m2, b3_same, 3);
    prefix_mixed_case("al", 2, m2, b3_last, 3);
    /* A shared start shorter than the amount every entry takes away, which
       is refused rather than applied: filling it in would delete more of
       the line than it put back. */
    static const char* m3[] = { "ab", "ax" };
    static const ssize_t b2[] = { 2, 2 };
    static const ssize_t b3[] = { 3, 3 };
    prefix_mixed_case("zz", 2, m3, b2, 2);
    prefix_mixed_case("zzz", 3, m3, b3, 2);
  }

  /* Replacements longer than the 256 byte buffer the shared start is copied
     into. The first pair share more than fits, so the answer is cut at 256.
     The second pair are built so that the 256th byte falls in the middle of
     a three byte character, which is the case that halves one. */
  {
    static char long_a[600], long_b[600], utf_a[600], utf_b[600];
    memset(long_a, 'x', 500); long_a[500] = 'a'; long_a[501] = 0;
    memset(long_b, 'x', 500); long_b[500] = 'b'; long_b[501] = 0;
    /* 255 filler bytes, then repeated U+65E5, so byte 256 is inside one. */
    memset(utf_a, 'y', 255);
    ssize_t k = 255;
    for (int i = 0; i < 40; i++) { utf_a[k++] = (char)0xE6; utf_a[k++] = (char)0x97; utf_a[k++] = (char)0xA5; }
    utf_a[k] = 0;
    memcpy(utf_b, utf_a, (size_t)k + 1);
    utf_b[k - 1] = (char)0xAC;  /* differs only after the cut */
    static const char* lp[2];
    static const ssize_t lb[] = { 1, 1 };
    lp[0] = long_a; lp[1] = long_b;
    prefix_mixed_case("x", 1, lp, lb, 2);
    lp[0] = utf_a; lp[1] = utf_b;
    prefix_mixed_case("y", 1, lp, lb, 2);
  }

  /* More orders for the sort, including entries that fold to the same bytes
     and entries of equal length. */
  {
    static const char* s4[] = { "B", "a", "C", "b", "A" };
    static const char* s5[] = { "zz", "z", "zzz", "" };
    static const char* s6[] = { "\xe6\x97\xa5", "ab", "a" };
    sort_case(s4, 5);
    sort_case(s5, 4);
    sort_case(s6, 3);
  }

  static const char* wadd1[] = { "world", "wonder" };
  static const char* wadd2[] = { "my file.txt" };
  static const char* winputs[] = {
    "hello", "hello wor", "cd /usr/lo", "a b c", "", "x",
    "ls 'my fi", "ls \"my fi", "echo a\\ b", "cmd 'quoted' arg",
    "a'b", "'unclosed", "\"unclosed", "esc\\'ape", "one\\ two",
    "\xe6\x97\xa5\xe6\x9c\xac", "path/to/f",
  };
  for (int i = 0; i < (int)(sizeof(winputs)/sizeof(winputs[0])); i++) {
    const ssize_t len = (ssize_t)strlen(winputs[i]);
    for (int cls = 0; cls < NCLASSES; cls++) {
      word_case(winputs[i], len, cls, wadd1, 2, false, 0, NULL);
      word_case(winputs[i], len, cls, wadd1, 2, true, '\\', NULL);
      word_case(winputs[i], len, cls, wadd2, 1, true, '\\', "'\"");
      word_case(winputs[i], len, cls, wadd2, 1, true, '^', "'");
    }
    /* A cursor inside the line, so that the text after it is taken into
       account when working out how much to delete. */
    if (len >= 2) {
      word_case(winputs[i], len - 2, 0, wadd1, 2, false, 0, NULL);
      word_case(winputs[i], len - 2, 0, wadd1, 2, true, '\\', NULL);
    }
  }
  return 0;
}
