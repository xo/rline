/* Print what the C functions in isocline's attr.c and the color helpers in
   term_color.c return, so that the Go port can be checked against them.

   This file includes isocline.c, which is the unity build of the whole
   library, because those functions are static there.

   The output is a golden file. The corpus is fixed, so the output does not
   change between runs. */

/* These must come before any system header. */
#define _XOPEN_SOURCE 700
#define _DEFAULT_SOURCE

#include "isocline.c"

#include <stdio.h>
#include <stdint.h>
#include <string.h>
#include <stdlib.h>

/* isocline keeps its allocator inside its environment, which the probe does
   not create. Give it the standard one instead. */
static alloc_t probe_mem = { &malloc, &realloc, &free };

static void print_str(const char* s) {
  if (*s == 0) { printf("-"); return; }
  for (const unsigned char* p = (const unsigned char*)s; *p != 0; p++) printf("%02x", *p);
}

/* An attr_t as its six fields. The port does not copy the bit packing, so the
   fields are the thing to compare. The three valued fields hold 1, 0 or -1. */
static void print_attr(attr_t a) {
  printf("%08x %08x %d %d %d %d",
    (unsigned)a.x.color, (unsigned)a.x.bgcolor,
    (int)a.x.bold, (int)a.x.italic, (int)a.x.reverse, (int)a.x.underline);
}

/* SGR parameter strings, without the leading escape. */
static const char* sgr_corpus[] = {
  "", "0", "1", "3", "4", "7", "22", "23", "24", "27", "39", "49",
  "30", "37", "40", "47", "90", "97", "100", "107",
  "1;4", "1;31", "31;42", "0;1;4;7", "1;22", "31;39",
  "38;5;0", "38;5;7", "38;5;8", "38;5;15", "38;5;16", "38;5;231", "38;5;255",
  "38;5;256", "38;5;999", "48;5;33", "38:5:33", "48:5:33",
  "38;2;255;0;0", "38;2;0;255;0", "38;2;1;2;3", "48;2;10;20;30",
  "38:2:255:0:0", "38;2;300;400;500", "38;2;-1;0;0",
  "38", "48", "38;", "38;5", "38;5;", "38;2;1;2", "5", "8", "99", "255",
  "1;;4", ";", ";;", "0;", "x", "1x", "  1", "01", "001", "1 ;4"
};
#define NSGR ((int)(sizeof(sgr_corpus)/sizeof(sgr_corpus[0])))

/* Full escape sequences, for attr_from_esc_sgr. */
static const char* esc_corpus[] = {
  "\x1B[m", "\x1B[0m", "\x1B[1m", "\x1B[31m", "\x1B[1;4m", "\x1B[38;5;33m",
  "\x1B[38;2;1;2;3m", "\x1B[", "\x1B[m ", "\x1B]0m", "[1m", "\x1B[1",
  "\x1B", "", "m", "\x1B[0;1;4;7;31;42m"
};
#define NESC ((int)(sizeof(esc_corpus)/sizeof(esc_corpus[0])))

/* Dump every entry of an attribute buffer. */
static void dump_abuf(const char* tag, int step, attrbuf_t* ab) {
  ssize_t n = attrbuf_len(ab);
  printf("abuf %s %d %d", tag, step, (int)n);
  const attr_t* attrs = attrbuf_attrs(ab, n);
  for (ssize_t i = 0; i < n; i++) {
    printf(" ");
    print_attr(attrs[i]);
  }
  printf("\n");
}

static attr_t sgr(const char* s) {
  return attr_from_sgr(s, (ssize_t)strlen(s));
}

int main(void) {
  /* Colors. */
  static const uint32_t hexes[] = {
    0, 1, 0xffffff, 0x1000000, 0x1234567, 0xabcdef, 0x808080, 0xffffffff
  };
  for (int i = 0; i < (int)(sizeof(hexes)/sizeof(hexes[0])); i++) {
    printf("rgb %08x %08x\n", hexes[i], (unsigned)ic_rgb(hexes[i]));
  }
  static const ssize_t comps[] = { -1000, -1, 0, 1, 127, 128, 254, 255, 256, 1000 };
  const int ncomp = (int)(sizeof(comps)/sizeof(comps[0]));
  for (int r = 0; r < ncomp; r++) {
    for (int g = 0; g < ncomp; g++) {
      for (int b = 0; b < ncomp; b++) {
        printf("rgbx %d %d %d %08x\n", (int)comps[r], (int)comps[g], (int)comps[b],
               (unsigned)ic_rgbx(comps[r], comps[g], comps[b]));
      }
    }
  }
  for (ssize_t i = -2; i <= 258; i++) {
    printf("ansi256 %d %08x\n", (int)i, (unsigned)color_from_ansi256(i));
  }

  /* Fixed attributes. */
  printf("none "); print_attr(attr_none()); printf("\n");
  printf("default "); print_attr(attr_default()); printf("\n");
  for (int i = 0; i < 16; i++) {
    printf("fromcolor %d ", i); print_attr(attr_from_color((ic_color_t)i)); printf("\n");
  }
  printf("isnone %d %d\n", attr_is_none(attr_none()) ? 1 : 0, attr_is_none(attr_default()) ? 1 : 0);

  /* SGR parsing. */
  for (int i = 0; i < NSGR; i++) {
    printf("sgr "); print_str(sgr_corpus[i]); printf(" ");
    print_attr(sgr(sgr_corpus[i])); printf("\n");
  }
  for (int i = 0; i < NESC; i++) {
    printf("escsgr "); print_str(esc_corpus[i]); printf(" ");
    print_attr(attr_from_esc_sgr(esc_corpus[i], (ssize_t)strlen(esc_corpus[i]))); printf("\n");
  }

  /* Combining. Every pair from a smaller set. */
  static const char* pairs[] = {
    "", "0", "1", "22", "31", "39", "42", "49", "4;7", "38;5;33", "38;2;1;2;3", "3"
  };
  const int npair = (int)(sizeof(pairs)/sizeof(pairs[0]));
  for (int i = 0; i < npair; i++) {
    for (int j = 0; j < npair; j++) {
      printf("update "); print_str(pairs[i]); printf(" "); print_str(pairs[j]); printf(" ");
      print_attr(attr_update_with(sgr(pairs[i]), sgr(pairs[j]))); printf("\n");
      printf("iseq "); print_str(pairs[i]); printf(" "); print_str(pairs[j]);
      printf(" %d\n", attr_is_eq(sgr(pairs[i]), sgr(pairs[j])) ? 1 : 0);
    }
  }

  /* The attribute buffer. Dump the whole buffer after every step, so a wrong
     shift shows up. */
  alloc_t* mem = &probe_mem;
  {
    attrbuf_t* ab = attrbuf_new(mem);
    int step = 0;
    dump_abuf("fresh", step++, ab);
    attrbuf_set_at(ab, 0, 4, sgr("31"));
    dump_abuf("fresh", step++, ab);
    attrbuf_set_at(ab, 2, 3, sgr("32"));
    dump_abuf("fresh", step++, ab);
    attrbuf_update_at(ab, 1, 3, sgr("1"));
    dump_abuf("fresh", step++, ab);
    attrbuf_insert_at(ab, 2, 2, sgr("4"));
    dump_abuf("fresh", step++, ab);
    attrbuf_clear(ab);
    dump_abuf("fresh", step++, ab);
    attrbuf_free(ab);
  }
  {
    /* Deletion, which is where the C moves the wrong number of bytes. Use a
       long run so the effect is visible. */
    attrbuf_t* ab = attrbuf_new(mem);
    for (int i = 0; i < 24; i++) {
      attrbuf_set_at(ab, i, 1, attr_from_color((ic_color_t)(i + 1)));
    }
    int step = 0;
    dump_abuf("del", step++, ab);
    attrbuf_delete_at(ab, 4, 2);
    dump_abuf("del", step++, ab);
    attrbuf_delete_at(ab, 0, 1);
    dump_abuf("del", step++, ab);
    attrbuf_delete_at(ab, 10, 100);
    dump_abuf("del", step++, ab);
    attrbuf_delete_at(ab, 100, 1);
    dump_abuf("del", step++, ab);
    attrbuf_delete_at(ab, 0, 0);
    dump_abuf("del", step++, ab);
    attrbuf_free(ab);
  }
  {
    /* Appending, which writes to a stringbuf at the same time. */
    attrbuf_t* ab = attrbuf_new(mem);
    stringbuf_t* sb = sbuf_new(mem);
    int step = 0;
    printf("append %d %d\n", step++, (int)attrbuf_append_n(sb, ab, "abc", 3, sgr("31")));
    dump_abuf("app", 0, ab);
    printf("append %d %d\n", step++, (int)attrbuf_append_n(sb, ab, "de", 2, sgr("1")));
    dump_abuf("app", 1, ab);
    printf("append %d %d\n", step++, (int)attrbuf_append_n(sb, ab, "", 0, sgr("32")));
    dump_abuf("app", 2, ab);
    printf("append %d %d\n", step++, (int)attrbuf_append_n(sb, NULL, "fg", 2, sgr("32")));
    dump_abuf("app", 3, ab);
    printf("appendstr "); print_str(sbuf_string(sb)); printf("\n");
    sbuf_free(sb);
    attrbuf_free(ab);
  }
  return 0;
}
