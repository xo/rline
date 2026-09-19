/* Print what isocline's highlight.c produces, so that the Go port can be
   checked against it.

   ic_highlight_env_s is a plain structure and the paths used here read only
   its fields, so the probe fills one in rather than starting an editor. */

#define _XOPEN_SOURCE 700
#define _DEFAULT_SOURCE

#include "isocline.c"

#include <stdio.h>
#include <string.h>
#include <stdlib.h>

static alloc_t probe_mem = { &malloc, &realloc, &free };

static void print_str(const char* s) {
  if (*s == 0) { printf("-"); return; }
  for (const unsigned char* p = (const unsigned char*)s; *p != 0; p++) {
    if (*p < 0x20 || *p >= 0x7f || *p == ' ') printf("\\x%02x", *p);
    else printf("%c", *p);
  }
}

static void print_attrs(attrbuf_t* ab, ssize_t n) {
  printf("%d", (int)n);
  const attr_t* attrs = attrbuf_attrs(ab, n);
  for (ssize_t i = 0; i < n; i++) {
    printf(" %08x/%08x/%d/%d/%d/%d",
      (unsigned)attrs[i].x.color, (unsigned)attrs[i].x.bgcolor,
      (int)attrs[i].x.bold, (int)attrs[i].x.italic,
      (int)attrs[i].x.reverse, (int)attrs[i].x.underline);
  }
}

/* Strings to match braces in. */
static const char* lines[] = {
  "", "a", "()", "(", ")", "()()", "(())", "(()", "())", "([)]", "([])",
  "{[()]}", "{[(])}", "((()))", ")(", "a(b)c", "(a[b]c)", "]", "[",
  "(((((", ")))))", "([{}])", "([{}]", "{}{}{}", "(]", "[)",
  "a(b[c]d)e", "((a)", "(a))", "no braces here"
};
#define NLINES ((int)(sizeof(lines)/sizeof(lines[0])))

static const char* bracesets[] = { "()[]{}", "()", "<>", "", "()[]" };
#define NBRACES ((int)(sizeof(bracesets)/sizeof(bracesets[0])))

int main(void) {
  attr_t match_attr = attr_from_sgr("1", 1);
  attr_t error_attr = attr_from_sgr("31", 2);

  /* find_matching_brace over every line, brace set and cursor position. */
  for (int b = 0; b < NBRACES; b++) {
    for (int i = 0; i < NLINES; i++) {
      const ssize_t len = (ssize_t)strlen(lines[i]);
      for (ssize_t cp = -1; cp <= len + 1; cp++) {
        bool balanced = false;
        ssize_t m = find_matching_brace(lines[i], cp, bracesets[b], &balanced);
        printf("match %d ", b);
        print_str(lines[i]);
        printf(" %d %d %d\n", (int)cp, (int)m, balanced ? 1 : 0);
      }
    }
  }

  /* highlight_match_braces over the same. */
  for (int b = 0; b < NBRACES; b++) {
    for (int i = 0; i < NLINES; i++) {
      const ssize_t len = (ssize_t)strlen(lines[i]);
      for (ssize_t cp = -1; cp <= len + 1; cp++) {
        attrbuf_t* ab = attrbuf_new(&probe_mem);
        if (len > 0) { attrbuf_set_at(ab, 0, len, attr_none()); }
        highlight_match_braces(lines[i], ab, cp, bracesets[b], match_attr, error_attr);
        printf("braces %d ", b);
        print_str(lines[i]);
        printf(" %d ", (int)cp);
        print_attrs(ab, attrbuf_len(ab));
        printf("\n");
        attrbuf_free(ab);
      }
    }
  }

  /* ic_highlight, including the negative positions that mean characters
     rather than bytes. */
  {
    term_t* term = term_new(&probe_mem, NULL, true, true, 1);
    bbcode_t* bb = bbcode_new(&probe_mem, term);
    static const char* inputs[] = {
      "hello", "\xc3\xa9\xc3\xa9\xc3\xa9", "a\xe6\x97\xa5" "b", "abcdef"
    };
    static const long poss[] = { -6, -3, -1, 0, 1, 3, 5, 6, 100 };
    static const long counts[] = { -3, -1, 0, 1, 2, 100 };
    for (int i = 0; i < (int)(sizeof(inputs)/sizeof(inputs[0])); i++) {
      for (int p = 0; p < (int)(sizeof(poss)/sizeof(poss[0])); p++) {
        for (int c = 0; c < (int)(sizeof(counts)/sizeof(counts[0])); c++) {
          const ssize_t len = (ssize_t)strlen(inputs[i]);
          attrbuf_t* ab = attrbuf_new(&probe_mem);
          attrbuf_set_at(ab, 0, len, attr_none());
          ic_highlight_env_t henv;
          henv.attrs = ab; henv.input = inputs[i]; henv.input_len = len;
          henv.bbcode = bb; henv.mem = &probe_mem;
          henv.cached_cpos = 0; henv.cached_upos = 0;
          ic_highlight(&henv, poss[p], counts[c], "bold");
          printf("hl %d %d %d ", i, (int)poss[p], (int)counts[c]);
          print_attrs(ab, attrbuf_len(ab));
          printf(" %d %d\n", (int)henv.cached_upos, (int)henv.cached_cpos);
          attrbuf_free(ab);
        }
      }
    }
    /* ic_highlight_formatted, where the markup describes the same text. */
    static const char* fmts[][2] = {
      { "hello", "[b]he[/b]llo" },
      { "hello", "[red]hello[/]" },
      { "hello", "[b]toolong[/b]" },
      { "hello", "[b]hi[/b]" },
      /* { "hello", "" } is left out on purpose. An empty format leaves the
         attribute buffer with nothing in it, and the loop in
         ic_highlight_formatted then reads the slot just past the end, because
         attrbuf_attr_at tests pos > count where it means pos >= count. That
         slot was never written, so the answer is a heap pointer and changes
         between runs. There is nothing there to compare against. */
      { "", "[b]x[/b]" },
    };
    for (int i = 0; i < (int)(sizeof(fmts)/sizeof(fmts[0])); i++) {
      const ssize_t len = (ssize_t)strlen(fmts[i][0]);
      attrbuf_t* ab = attrbuf_new(&probe_mem);
      if (len > 0) { attrbuf_set_at(ab, 0, len, attr_none()); }
      ic_highlight_env_t henv;
      henv.attrs = ab; henv.input = fmts[i][0]; henv.input_len = len;
      henv.bbcode = bb; henv.mem = &probe_mem;
      henv.cached_cpos = 0; henv.cached_upos = 0;
      ic_highlight_formatted(&henv, fmts[i][0], fmts[i][1]);
      printf("hlfmt %d ", i);
      print_attrs(ab, attrbuf_len(ab));
      printf("\n");
      attrbuf_free(ab);
    }
    bbcode_free(bb);
    term_free(term);
  }
  return 0;
}
