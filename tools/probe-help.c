/* Print the lines that isocline's help screen is built from, so that the Go
   port can be checked against them.

   edit_show_help hands each line to bbcode_printf, which applies the markup
   and writes to a terminal. This prints the same strings instead, before the
   markup is applied, so the table and the formatting can be checked without
   a terminal. Rendering the markup is bbcode's job and is already checked
   against its own corpus.

   The output is a golden file. */
#define _XOPEN_SOURCE 700
#define _DEFAULT_SOURCE

#include "isocline.c"

#include <stdio.h>

int main(void) {
  /* The banner, exactly as it is handed to bbcode_println. */
  printf("banner ");
  for (const unsigned char* p = (const unsigned char*)help_initial; *p != 0; p++) {
    printf("%02x", *p);
  }
  printf("\n");

  /* Then each row, formatted the way edit_show_help formats it. */
  for (ssize_t i = 0; help[i] != NULL && help[i+1] != NULL; i += 2) {
    char line[512];
    if (help[i][0] == 0) {
      snprintf(line, sizeof(line), "[ic-info]%s[/]\n", help[i+1]);
    }
    else {
      snprintf(line, sizeof(line), "  [ic-emphasis]%-13s[/][ansi-lightgray]%s%s[/]\n",
               help[i], (help[i+1][0] == 0 ? "" : ": "), help[i+1]);
    }
    printf("row ");
    for (const unsigned char* p = (const unsigned char*)line; *p != 0; p++) {
      printf("%02x", *p);
    }
    printf("\n");
  }
  return 0;
}
