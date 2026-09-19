/* Print what the C functions in isocline's common.c return, so that the Go
   port can be checked against them.

   This file includes isocline.c, which is the unity build of the whole
   library. The functions in common.c are static there, so including the
   source is the only way to reach them.

   The output is a golden file. Every line is one call and its result.
   The corpus is fixed, so the output does not change between runs. */
/* These must come before any system header. isocline.c sets them too, but by
   then the C library headers have already fixed which interfaces they expose,
   and completers.c needs lstat and the S_IF* constants. */
#define _XOPEN_SOURCE 700
#define _DEFAULT_SOURCE

#include "isocline.c"

#include <stdio.h>
#include <stdint.h>

static void print_bytes(const uint8_t* b, int n) {
  for (int i = 0; i < n; i++) printf("%02x", b[i]);
}

/* Bytes that sit on a boundary of the decoder. */
static const uint8_t interesting[] = {
  0x00, 0x01, 0x41, 0x7f, 0x80, 0x81, 0x8f, 0x90,
  0x9f, 0xa0, 0xbf, 0xc0, 0xc1, 0xc2, 0xed, 0xff
};
#define NINTERESTING ((int)(sizeof(interesting)/sizeof(interesting[0])))

static const uint8_t tail[] = { 0x00, 0x80, 0xbf, 0xc0 };
#define NTAIL ((int)(sizeof(tail)/sizeof(tail[0])))

static void decode_case(const uint8_t* b, int n) {
  ssize_t count = 0;
  unicode_t u = unicode_from_qutf8(b, n, &count);
  printf("decode ");
  print_bytes(b, n);
  printf(" %d %08x\n", (int)count, u);
}

static void encode_case(unicode_t u) {
  uint8_t buf[5];
  unicode_to_qutf8(u, buf);
  /* Print the whole buffer. unicode_to_qutf8 zero fills it first, so the
     trailing zeros are part of the result. Measuring the length by looking for
     the first zero byte cannot work, because the raw byte 0x00 encodes to a
     single zero byte. */
  printf("encode %08x ", u);
  print_bytes(buf, 5);
  printf("\n");
}

/* The string corpus. These cover ASCII case, the boundary characters around
   the letters, bytes above 0x7f, and UTF-8 text whose case the ASCII rules
   cannot change. */
static const char* corpus[] = {
  "", "a", "A", "ab", "AB", "aB", "Ab", "abc", "ABC", "abd", "abcd",
  "z", "Z", "[", "{", "@", "`", "0", "9", "_", "~",
  "hello", "HELLO", "Hello", "h\xc3\xa9llo", "H\xc3\x89LLO",
  "\xe6\x97\xa5\xe6\x9c\xac", "a b", "ab ", " ab", "\xff", "a\xff" "b"
};
#define NCORPUS ((int)(sizeof(corpus)/sizeof(corpus[0])))

static void print_str(const char* s) {
  if (*s == 0) { printf("-"); return; }
  for (const unsigned char* p = (const unsigned char*)s; *p != 0; p++) printf("%02x", *p);
}

static void string_cases(void) {
  for (int i = 0; i < 256; i++) {
    printf("tolower %02x %02x\n", i, (unsigned char)ic_tolower((char)i));
  }
  for (int i = 0; i < NCORPUS; i++) {
    for (int j = 0; j < NCORPUS; j++) {
      const char* a = corpus[i];
      const char* b = corpus[j];
      printf("stricmp "); print_str(a); printf(" "); print_str(b);
      printf(" %d\n", ic_stricmp(a, b));
      printf("istarts "); print_str(a); printf(" "); print_str(b);
      printf(" %d\n", ic_istarts_with(a, b) ? 1 : 0);
      printf("starts "); print_str(a); printf(" "); print_str(b);
      printf(" %d\n", ic_starts_with(a, b) ? 1 : 0);
      printf("icontains "); print_str(a); printf(" "); print_str(b);
      printf(" %d\n", ic_icontains(a, b) ? 1 : 0);
      printf("contains "); print_str(a); printf(" "); print_str(b);
      printf(" %d\n", ic_contains(a, b) ? 1 : 0);
      for (int n = 0; n <= 4; n++) {
        printf("strnicmp "); print_str(a); printf(" "); print_str(b);
        printf(" %d %d\n", n, ic_strnicmp(a, b, n));
      }
    }
  }
}

/* Print the column width that isocline gives every code point, as ranges.
   mk_wcwidth lives in wcwidth.c, which stringbuf.c includes, so the unity
   build reaches it. Ranges keep the output small while covering every code
   point from U+0000 to U+10FFFF. */
static void width_ranges(void) {
  int prev = mk_wcwidth(0);
  unicode_t start = 0;
  for (unicode_t u = 1; u <= 0x110000; u++) {
    int w = (u <= 0x10ffff ? mk_wcwidth((int32_t)u) : prev - 1);
    if (w != prev) {
      printf("width %06x %06x %d\n", start, u - 1, prev);
      start = u;
      prev = w;
    }
  }
}

int main(void) {
  /* One byte. */
  for (int a = 0; a < 256; a++) {
    uint8_t b[1] = { (uint8_t)a };
    decode_case(b, 1);
  }
  /* Two bytes. */
  for (int a = 0; a < 256; a++) {
    for (int i = 0; i < NINTERESTING; i++) {
      uint8_t b[2] = { (uint8_t)a, interesting[i] };
      decode_case(b, 2);
    }
  }
  /* Three bytes. */
  for (int a = 0xe0; a <= 0xef; a++) {
    for (int i = 0; i < NINTERESTING; i++) {
      for (int j = 0; j < NTAIL; j++) {
        uint8_t b[3] = { (uint8_t)a, interesting[i], tail[j] };
        decode_case(b, 3);
      }
    }
  }
  /* Four bytes. */
  for (int a = 0xf0; a <= 0xf7; a++) {
    for (int i = 0; i < NINTERESTING; i++) {
      for (int j = 0; j < NTAIL; j++) {
        for (int k = 0; k < NTAIL; k++) {
          uint8_t b[4] = { (uint8_t)a, interesting[i], tail[j], tail[k] };
          decode_case(b, 4);
        }
      }
    }
  }
  /* Encoding. Every ASCII code point, then the boundaries, then the whole raw
     plane, then a stride across the rest. */
  for (unicode_t u = 0; u <= 0x7f; u++) encode_case(u);
  static const unicode_t bounds[] = {
    0x80, 0x7ff, 0x800, 0xd7ff, 0xd800, 0xdfff, 0xe000, 0xfffd, 0xffff,
    0x10000, 0xedfff, 0xee000, 0xee0ff, 0xee100, 0xfffff, 0x10ffff, 0x110000
  };
  for (int i = 0; i < (int)(sizeof(bounds)/sizeof(bounds[0])); i++) encode_case(bounds[i]);
  for (unicode_t u = 0xee000; u <= 0xee0ff; u++) encode_case(u);
  for (unicode_t u = 0x100; u <= 0x10ffff; u += 0x1111) encode_case(u);
  string_cases();
  width_ranges();
  return 0;
}
