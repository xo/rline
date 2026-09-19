/* Print what isocline's color reduction in term_color.c returns, so that the
   Go port can be checked against it.

   fmt_color_ex leaves the buffer untouched when the color is none or the
   terminal is monochrome, and term_color_ex hands it an uninitialized buffer,
   so the C reads uninitialized stack there. The probe zeroes the buffer first,
   which makes the corpus deterministic and records the intent. */

#define _XOPEN_SOURCE 700
#define _DEFAULT_SOURCE

#include "isocline.c"

#include <stdio.h>
#include <stdint.h>
#include <string.h>

static void print_esc(const char* s) {
  if (*s == 0) { printf("-"); return; }
  for (const unsigned char* p = (const unsigned char*)s; *p != 0; p++) {
    if (*p == 0x1b) { printf("\\e"); continue; }
    printf("%c", *p);
  }
}

/* Colors worth testing: the palette codes, values around the edges, every
   entry of the 256 color table as an RGB value, and a stride across the cube. */
static ic_color_t colors[4096];
static int ncolors = 0;

static void add(ic_color_t c) {
  if (ncolors < (int)(sizeof(colors)/sizeof(colors[0]))) colors[ncolors++] = c;
}

static void build_colors(void) {
  for (int i = 0; i <= 108; i++) add((ic_color_t)i);
  for (int i = 0; i < 256; i++) add(ic_rgb(ansi256[i]));
  static const uint32_t edges[] = {
    0x000000, 0x010101, 0x040404, 0x050505, 0x7f7f7f, 0x808080, 0xc3c3c3,
    0xc4c4c4, 0xfefefe, 0xffffff, 0xff0000, 0x00ff00, 0x0000ff, 0xffff00,
    0x00ffff, 0xff00ff, 0x800000, 0x008000, 0x000080, 0x123456, 0xabcdef,
    0xc0c0c4, 0xc4c0c0, 0x040800, 0x000408
  };
  for (int i = 0; i < (int)(sizeof(edges)/sizeof(edges[0])); i++) add(ic_rgb(edges[i]));
  /* A stride across the cube. 0x191919 keeps it under the array bound. */
  for (uint32_t v = 0; v <= 0xFFFFFF; v += 0x0D1B17) add(ic_rgb(v));
}

int main(void) {
  build_colors();
  /* The reduction functions on their own. */
  for (int i = 0; i < ncolors; i++) {
    ic_color_t c = colors[i];
    printf("ansi16 %08x %d\n", (unsigned)c, color_to_ansi16(c));
    printf("ansi8 %08x %d\n", (unsigned)c, color_to_ansi8(c));
    if (color_is_rgb(c)) {
      printf("to256 %08x %d\n", (unsigned)c, rgb_to_ansi256(c));
    }
  }
  /* The escape sequence each palette produces. */
  for (int p = MONOCHROME; p <= ANSIRGB; p++) {
    for (int i = 0; i < ncolors; i++) {
      for (int bg = 0; bg <= 1; bg++) {
        char buf[129];
        memset(buf, 0, sizeof(buf));
        fmt_color_ex(buf, 128, (palette_t)p, colors[i], bg != 0);
        printf("fmt %d %08x %d ", p, (unsigned)colors[i], bg);
        print_esc(buf);
        printf("\n");
      }
    }
  }
  /* is_grayish, which decides whether a substitution is penalised. */
  for (int i = 0; i < ncolors; i++) {
    int r, g, b;
    if (!color_is_rgb(colors[i])) continue;
    color_to_rgb(colors[i], &r, &g, &b);
    printf("grayish %08x %d\n", (unsigned)colors[i], is_grayish(r, g, b) ? 1 : 0);
  }
  return 0;
}
