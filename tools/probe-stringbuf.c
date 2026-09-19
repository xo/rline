/* Print what the C functions in isocline's wcwidth.c and stringbuf.c return,
   so that the Go port can be checked against them.

   This file includes isocline.c, which is the unity build of the whole
   library. The functions of both modules are static there, so including the
   source is the only way to reach them.

   The output is a golden file. Every line is one call and its result. The
   corpus is fixed, so the output does not change between runs. */
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

/* Print a string as hex. An empty string prints as "-", because a field of an
   output line cannot be empty. */
static void print_str(const char* s) {
  if (s == NULL) { printf("!"); return; }
  if (*s == 0) { printf("-"); return; }
  for (const unsigned char* p = (const unsigned char*)s; *p != 0; p++) {
    printf("%02x", *p);
  }
}

/* ------------------------------------------------------------------------
   The string corpus.

   These cover ASCII, whitespace and word boundaries, the control bytes, every
   length of UTF-8 sequence, wide and zero width characters, invalid and
   truncated sequences, each shape of escape sequence, and several lines.
   ------------------------------------------------------------------------ */
static const char* corpus[] = {
  "",
  "a",
  "ab",
  "hello world",
  "  leading",
  "trailing  ",
  "a\tb\nc\rd",
  "one two three",
  "foo-bar_baz qux.quux",
  "a_b-c9",
  /* UTF-8 of every length. */
  "\xc3\xa9",                          /* U+00E9, width 1 */
  "e\xcc\x81",                         /* e + U+0301, widths 1 and 0 */
  "\xe6\x97\xa5\xe6\x9c\xac\xe8\xaa\x9e", /* wide CJK */
  "\xf0\x9f\x98\x80",                  /* U+1F600, wide */
  "\xf0\x9f\x8f\xb4",                  /* U+1F3F4, wide */
  "\xc2\x80",                          /* U+0080, a C1 control */
  "\xc2\xa0",                          /* no-break space */
  "\xef\xbb\xbf",                      /* U+FEFF, zero width */
  "\xe2\x80\x8b",                      /* U+200B, zero width */
  "\xed\x80\x80",                      /* the pair the QUTF-8 decoder refuses */
  "\xe0\x80\x80",                      /* overlong, decodes to U+0000 here */
  /* Invalid and truncated. */
  "\xff\xfe",
  "\xc3",
  "\xe6\x97",
  "\xf0\x9f\x98",
  "a\x80\x62",
  "\x80\x80\x80",
  /* Control bytes. */
  "\x7f",
  "\x01\x02",
  /* Escape sequences. */
  "a\x1b[31mred\x1b[0m",
  "\x1b]0;title\x07x",
  "\x1b]0;title\x1b\\x",
  "\x1bPq\x1b\\z",
  "\x1bX data\x02z",
  "\x1b^pm\x07z",
  "\x1b_apc\x07z",
  "\x1b" "7abc",
  "\x1b%Gabc",
  "\x1b#8z",
  "\x1b",
  "\x1b[",
  "\x1b[31",
  /* Several lines. */
  "line1\nline2\nline3",
  "a\nbb\nccc\n",
  "\n",
  "\n\n",
  "abc def\nghi jkl",
  "\xe6\x97\xa5\n\xe6\x9c\xac",
  "0123456789012345678901234567890123456789",
};
#define NCORPUS ((int)(sizeof(corpus)/sizeof(corpus[0])))

/* ------------------------------------------------------------------------
   Character classes
   ------------------------------------------------------------------------ */
static void class_cases(void) {
  for (int i = 0; i < 256; i++) {
    char c = (char)i;
    /* Call with a length of one, which is what the callers use, and then with
       a length of two and of zero, because several of these test the length. */
    for (int n = 0; n <= 2; n++) {
      char s[3]; s[0] = c; s[1] = 'x'; s[2] = 0;
      printf("class %02x %d %d %d %d %d %d %d %d %d %d\n", i, n,
        ic_char_is_white(s, n) ? 1 : 0,
        ic_char_is_nonwhite(s, n) ? 1 : 0,
        ic_char_is_separator(s, n) ? 1 : 0,
        ic_char_is_nonseparator(s, n) ? 1 : 0,
        ic_char_is_digit(s, n) ? 1 : 0,
        ic_char_is_hexdigit(s, n) ? 1 : 0,
        ic_char_is_letter(s, n) ? 1 : 0,
        ic_char_is_idletter(s, n) ? 1 : 0,
        ic_char_is_filename_letter(s, n) ? 1 : 0);
    }
  }
}

/* ------------------------------------------------------------------------
   Width, navigation and search over the corpus
   ------------------------------------------------------------------------ */
static const ssize_t fit_widths[] = { -1, 0, 1, 2, 3, 5, 8, 20 };
#define NFIT ((int)(sizeof(fit_widths)/sizeof(fit_widths[0])))

static void string_cases(void) {
  for (int i = 0; i < NCORPUS; i++) {
    const char* s = corpus[i];
    const ssize_t len = ic_strlen(s);

    printf("width "); print_str(s);
    printf(" %zd\n", str_column_width(s));

    for (int f = 0; f < NFIT; f++) {
      printf("fit "); print_str(s);
      printf(" %zd %zd %zd\n", fit_widths[f],
        str_skip_until_fit(s, fit_widths[f]),
        str_take_while_fit(s, fit_widths[f]));
    }

    /* One position past each end, to pin how the code clamps. */
    for (ssize_t pos = -1; pos <= len + 1; pos++) {
      ssize_t cw = 0;
      ssize_t ofs = 0;

      if (pos >= 0) {
        ofs = str_next_ofs(s, len, pos, &cw);
        printf("next "); print_str(s);
        printf(" %zd %zd %zd\n", pos, ofs, cw);

        cw = 0;
        ofs = str_prev_ofs(s, pos, &cw);
        printf("prev "); print_str(s);
        printf(" %zd %zd %zd\n", pos, ofs, cw);

        ssize_t esclen = 0;
        bool ok = skip_esc(s + pos, len - pos, &esclen);
        printf("esc "); print_str(s);
        printf(" %zd %d %zd\n", pos, ok ? 1 : 0, esclen);
      }

      printf("find "); print_str(s);
      printf(" %zd %zd %zd %zd %zd %zd %zd\n", pos,
        str_find_line_start(s, len, pos),
        str_find_line_end(s, len, pos),
        str_find_word_start(s, len, pos),
        str_find_word_end(s, len, pos),
        str_find_ws_word_start(s, len, pos),
        str_find_ws_word_end(s, len, pos));
    }
  }
}

/* ------------------------------------------------------------------------
   Rows and columns
   ------------------------------------------------------------------------ */
static const ssize_t termws[]   = { 0, 5, 10, 20, 80 };
static const ssize_t promptws[] = { 0, 3 };
static const ssize_t cpromptws[]= { 0, 2 };
static const ssize_t newtermws[]= { 4, 7, 40 };
#define NTERMW  ((int)(sizeof(termws)/sizeof(termws[0])))
#define NPROMPTW ((int)(sizeof(promptws)/sizeof(promptws[0])))
#define NCPROMPTW ((int)(sizeof(cpromptws)/sizeof(cpromptws[0])))
#define NNEWTERMW ((int)(sizeof(newtermws)/sizeof(newtermws[0])))

static void print_rc(const rowcol_t* rc) {
  printf(" %zd %zd %zd %zd %d %d", rc->row, rc->col, rc->row_start, rc->row_len,
    rc->first_on_row ? 1 : 0, rc->last_on_row ? 1 : 0);
}

static void rowcol_cases(void) {
  for (int i = 0; i < NCORPUS; i++) {
    const char* s = corpus[i];
    const ssize_t len = ic_strlen(s);
    for (int t = 0; t < NTERMW; t++) {
      for (int p = 0; p < NPROMPTW; p++) {
        for (int c = 0; c < NCPROMPTW; c++) {
          const ssize_t termw = termws[t];
          const ssize_t promptw = promptws[p];
          const ssize_t cpromptw = cpromptws[c];

          for (ssize_t pos = 0; pos <= len; pos++) {
            rowcol_t rc;
            ssize_t rows = str_get_rc_at_pos(s, len, termw, promptw, cpromptw, pos, &rc);
            printf("rc "); print_str(s);
            printf(" %zd %zd %zd %zd %zd", termw, promptw, cpromptw, pos, rows);
            print_rc(&rc);
            printf("\n");
          }

          for (ssize_t row = 0; row <= 4; row++) {
            for (ssize_t col = 0; col <= 6; col += 2) {
              printf("posrc "); print_str(s);
              printf(" %zd %zd %zd %zd %zd %zd\n", termw, promptw, cpromptw, row, col,
                str_get_pos_at_rc(s, len, termw, promptw, cpromptw, row, col));
            }
          }

          for (int n = 0; n < NNEWTERMW; n++) {
            for (ssize_t pos = 0; pos <= len; pos++) {
              rowcol_t rc;
              ssize_t rows = str_get_wrapped_rc_at_pos(s, len, termw, newtermws[n],
                promptw, cpromptw, pos, &rc);
              printf("wrc "); print_str(s);
              printf(" %zd %zd %zd %zd %zd %zd", termw, newtermws[n], promptw, cpromptw, pos, rows);
              print_rc(&rc);
              printf("\n");
            }
          }
        }
      }
    }
  }
}

/* ------------------------------------------------------------------------
   Buffer edit operations
   ------------------------------------------------------------------------ */
static stringbuf_t* sbuf_of(const char* s) {
  stringbuf_t* sb = sbuf_new(&probe_mem);
  sbuf_append(sb, s);
  return sb;
}

/* Print the buffer by its length, not as a C string.

   sbuf_split_at sets the new count of the left buffer but never writes the
   terminating zero, so after a split the buffer is not a valid C string and
   sbuf_string asserts. That function has no caller in isocline, so the bug
   never reaches a recorded session. Reading by length records the true
   content in every case. */
static void print_sbuf(stringbuf_t* sb) {
  const ssize_t n = sbuf_len(sb);
  if (n <= 0) { printf("-"); return; }
  for (ssize_t i = 0; i < n; i++) {
    printf("%02x", (unsigned char)sb->buf[i]);
  }
}

static const char* inserts[] = { "", "x", "xy", "\xc3\xa9", "\xe6\x97\xa5", "a\tb" };
#define NINSERTS ((int)(sizeof(inserts)/sizeof(inserts[0])))

static void buffer_cases(void) {
  for (int i = 0; i < NCORPUS; i++) {
    const char* s = corpus[i];
    const ssize_t len = ic_strlen(s);

    for (ssize_t pos = -1; pos <= len + 1; pos++) {
      for (int k = 0; k < NINSERTS; k++) {
        stringbuf_t* sb = sbuf_of(s);
        ssize_t np = sbuf_insert_at(sb, inserts[k], pos);
        printf("ins "); print_str(s); printf(" "); print_str(inserts[k]);
        printf(" %zd %zd ", pos, np); print_sbuf(sb); printf("\n");
        sbuf_free(sb);
      }

      for (ssize_t count = 0; count <= 3; count++) {
        stringbuf_t* sb = sbuf_of(s);
        sbuf_delete_at(sb, pos, count);
        printf("del "); print_str(s);
        printf(" %zd %zd ", pos, count); print_sbuf(sb); printf("\n");
        sbuf_free(sb);
      }

      /* sbuf_swap_char, sbuf_delete_char_at and sbuf_next all reach
         str_next_ofs, which reads s[pos] without checking that pos is not
         negative. Calling them at -1 reads out of bounds, so the corpus stops
         at zero for those three. sbuf_insert_at, sbuf_delete_at and
         sbuf_delete_char_before all test pos < 0 first, so -1 is safe there
         and the corpus keeps it. */
      if (pos >= 0) {
        stringbuf_t* sb = sbuf_of(s);
        ssize_t np = sbuf_swap_char(sb, pos);
        printf("swap "); print_str(s);
        printf(" %zd %zd ", pos, np); print_sbuf(sb); printf("\n");
        sbuf_free(sb);
      }
      {
        stringbuf_t* sb = sbuf_of(s);
        ssize_t np = sbuf_delete_char_before(sb, pos);
        printf("delbefore "); print_str(s);
        printf(" %zd %zd ", pos, np); print_sbuf(sb); printf("\n");
        sbuf_free(sb);
      }
      if (pos >= 0) {
        stringbuf_t* sb = sbuf_of(s);
        sbuf_delete_char_at(sb, pos);
        printf("delat "); print_str(s);
        printf(" %zd ", pos); print_sbuf(sb); printf("\n");
        sbuf_free(sb);
      }
      if (pos >= 0) {
        stringbuf_t* sb = sbuf_of(s);
        stringbuf_t* rest = sbuf_split_at(sb, pos);
        printf("split "); print_str(s);
        printf(" %zd ", pos); print_sbuf(sb); printf(" ");
        if (rest == NULL) printf("!"); else print_sbuf(rest);
        printf("\n");
        sbuf_free(rest);
        sbuf_free(sb);
      }
      if (pos >= 0) {
        stringbuf_t* sb = sbuf_of(s);
        ssize_t cw = 0;
        ssize_t np = sbuf_next(sb, pos, &cw);
        printf("bnext "); print_str(s);
        printf(" %zd %zd %zd\n", pos, np, cw);
        cw = 0;
        np = sbuf_prev(sb, pos, &cw);
        printf("bprev "); print_str(s);
        printf(" %zd %zd %zd\n", pos, np, cw);
        sbuf_free(sb);
      }
      if (pos >= 0) {
        stringbuf_t* sb = sbuf_of(s);
        printf("charat "); print_str(s);
        printf(" %zd %02x\n", pos, (unsigned char)sbuf_char_at(sb, pos));
        sbuf_free(sb);
      }
    }

    /* Decode to the locale encoding. Returns NULL for an empty buffer. */
    {
      stringbuf_t* sb = sbuf_of(s);
      char* d = sbuf_strdup_from_utf8(sb);
      printf("fromutf8 "); print_str(s); printf(" ");
      print_str(d);
      printf("\n");
      mem_free(&probe_mem, d);
      sbuf_free(sb);
    }
  }
}

/* ------------------------------------------------------------------------
   End overlap, tokens and the number parsers
   ------------------------------------------------------------------------ */
static const char* overlaps[] = {
  "", "a", "ab", "abc", "abcabc", "bc", "cab", "abcd", "\xe6\x97\xa5", "x"
};
#define NOVERLAPS ((int)(sizeof(overlaps)/sizeof(overlaps[0])))

static void overlap_cases(void) {
  for (int i = 0; i < NOVERLAPS; i++) {
    for (int j = 0; j < NOVERLAPS; j++) {
      printf("overlap "); print_str(overlaps[i]); printf(" "); print_str(overlaps[j]);
      printf(" %zd\n", ic_count_end_overlap(overlaps[i], overlaps[j]));
    }
  }
}

/* The class functions that ic_is_token takes, in a fixed order, so that the
   corpus can name one by index. */
static ic_is_char_class_fun_t* const token_classes[] = {
  &ic_char_is_letter, &ic_char_is_idletter, &ic_char_is_nonwhite,
  &ic_char_is_nonseparator, &ic_char_is_digit
};
#define NTOKENCLASSES ((int)(sizeof(token_classes)/sizeof(token_classes[0])))

static const char* tokens[] = { "fun", "function", "foo", "a", "12", "\xe6\x97\xa5" };
#define NTOKENS ((int)(sizeof(tokens)/sizeof(tokens[0])))

static void token_cases(void) {
  for (int i = 0; i < NCORPUS; i++) {
    const char* s = corpus[i];
    const ssize_t len = ic_strlen(s);
    for (long pos = -1; pos <= (long)len; pos++) {
      for (int c = 0; c < NTOKENCLASSES; c++) {
        printf("istoken "); print_str(s);
        printf(" %ld %d %ld\n", pos, c, ic_is_token(s, pos, token_classes[c]));
        for (int t = 0; t < NTOKENS; t++) {
          printf("mtoken "); print_str(s); printf(" "); print_str(tokens[t]);
          printf(" %ld %d %ld\n", pos, c,
            ic_match_token(s, pos, token_classes[c], tokens[t]));
        }
        printf("manytoken "); print_str(s);
        printf(" %ld %d %ld\n", pos, c,
          ic_match_any_token(s, pos, token_classes[c], tokens));
      }
      printf("prevchar "); print_str(s);
      printf(" %ld %ld\n", pos, ic_prev_char(s, pos));
      printf("nextchar "); print_str(s);
      printf(" %ld %ld\n", pos, ic_next_char(s, pos));
    }
  }
}

/* The number parsers leave their output unchanged when they fail, so the
   corpus records the value that was there before the call. */
static const char* numbers[] = {
  "", "0", "1", "12", "  12", "+5", "-5", "007", "1x", "x1", "12;34",
  "12;", ";34", "1;2;3", "2147483647", "4294967295", " 1 ; 2 "
};
#define NNUMBERS ((int)(sizeof(numbers)/sizeof(numbers[0])))

static void number_cases(void) {
  for (int i = 0; i < NNUMBERS; i++) {
    const char* s = numbers[i];
    ssize_t a = -111;
    bool ok = ic_atoz(s, &a);
    printf("atoz "); print_str(s); printf(" %d %zd\n", ok ? 1 : 0, a);

    ssize_t b = -111, c = -222;
    ok = ic_atoz2(s, &b, &c);
    printf("atoz2 "); print_str(s); printf(" %d %zd %zd\n", ok ? 1 : 0, b, c);

    uint32_t u = 4000000000u;
    ok = ic_atou32(s, &u);
    printf("atou32 "); print_str(s); printf(" %d %u\n", ok ? 1 : 0, u);
  }
}

int main(void) {
  class_cases();
  string_cases();
  rowcol_cases();
  buffer_cases();
  overlap_cases();
  token_cases();
  number_cases();
  return 0;
}
