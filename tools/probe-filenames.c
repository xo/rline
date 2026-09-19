/* Print what isocline's file name completion does, so that the Go port can be
   checked against it.

   This is the one module so far where recorded calls cannot be the whole
   oracle, because it reads the file system rather than a string. So the probe
   builds a fixed tree in a temporary directory, changes into it, and completes
   against it. Everything it prints is relative to that directory, so the
   temporary path never reaches the output.

   Directory order is whatever the file system gives, and it is not the same
   twice on every file system, so each result is sorted before it is printed.
   Nothing is lost: editline sorts the completions itself before it shows them.

   The colour settings are read once into static variables and cached, so the
   probe clears them between cases and sets the environment for each one.

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
#include <unistd.h>
#include <sys/stat.h>

static alloc_t probe_mem = { &malloc, &realloc, &free };

static void print_bytes(const uint8_t* b, ssize_t n) {
  if (n <= 0) { printf("-"); return; }
  for (ssize_t i = 0; i < n; i++) printf("%02x", b[i]);
}

static void print_str(const char* s) {
  if (s == NULL) { printf("!"); return; }
  print_bytes((const uint8_t*)s, (ssize_t)strlen(s));
}

static ic_env_t probe_env;

/* ------------------------------------------------------------------------
   Matching an extension
   ------------------------------------------------------------------------ */
static void extension_case(const char* name, const char* extensions) {
  printf("extmatch ");
  print_str(name);
  printf(" ");
  print_str(extensions);
  printf(" %d\n", match_extension(name, extensions) ? 1 : 0);
}

/* ------------------------------------------------------------------------
   Colouring an entry

   The settings are cached in static variables after the first look, so each
   case clears them first.
   ------------------------------------------------------------------------ */
static void reset_ls_colors(void) {
  cli_color = 0;
  ls_colors = NULL;
  lscolors = "exfxcxdxbxegedabagacad";
}

static void set_env(const char* name, const char* value) {
  if (value == NULL) unsetenv(name); else setenv(name, value, 1);
}

static void colorize_case(const char* clicolor, const char* gnu, const char* bsd,
                          int ft, const char* name, const char* ext,
                          char dirsep, bool no_lscolor) {
  reset_ls_colors();
  set_env("CLICOLOR", clicolor);
  set_env("LS_COLORS", gnu);
  set_env("LSCOLORS", bsd);
  stringbuf_t* sb = sbuf_new(&probe_mem);
  ls_colorize(no_lscolor, sb, (file_type_t)ft, name, ext, dirsep);
  printf("colorize ");
  print_str(clicolor); printf(" "); print_str(gnu); printf(" "); print_str(bsd);
  printf(" %d ", ft);
  print_str(name); printf(" "); print_str(ext);
  printf(" %02x %d ", (unsigned char)dirsep, no_lscolor ? 1 : 0);
  print_bytes((const uint8_t*)sbuf_string(sb), sbuf_len(sb));
  printf("\n");
  sbuf_free(sb);
}

/* ------------------------------------------------------------------------
   Completing against the tree
   ------------------------------------------------------------------------ */
typedef struct result_s {
  char replacement[512];
  char display[512];
  ssize_t before;
  ssize_t after;
} result_t;

static int compare_results(const void* a, const void* b) {
  const result_t* r1 = (const result_t*)a;
  const result_t* r2 = (const result_t*)b;
  int c = strcmp(r1->replacement, r2->replacement);
  if (c != 0) return c;
  return strcmp(r1->display, r2->display);
}

static void files_case(const char* prefix, char dirsep, const char* roots,
                       const char* extensions, bool no_lscolor) {
  reset_ls_colors();
  set_env("CLICOLOR", NULL);   /* colours off, so the display stays plain */
  set_env("LS_COLORS", NULL);
  set_env("LSCOLORS", NULL);

  memset(&probe_env, 0, sizeof(probe_env));
  probe_env.mem = &probe_mem;
  probe_env.no_lscolors = no_lscolor;
  completions_t* cms = completions_new(&probe_mem);
  probe_env.completions = cms;

  ic_completion_env_t cenv;
  memset(&cenv, 0, sizeof(cenv));
  cenv.env = &probe_env;
  cenv.input = prefix;
  cenv.cursor = (long)strlen(prefix);
  cenv.complete = &prim_add_completion;
  cenv.closure = NULL;
  cms->completer_max = 200;
  ic_complete_filename(&cenv, prefix, dirsep, roots, extensions);

  ssize_t n = completions_count(cms);
  result_t* results = (result_t*)calloc((size_t)(n > 0 ? n : 1), sizeof(result_t));
  for (ssize_t i = 0; i < n; i++) {
    snprintf(results[i].replacement, sizeof(results[i].replacement), "%s",
             cms->elems[i].replacement == NULL ? "" : cms->elems[i].replacement);
    snprintf(results[i].display, sizeof(results[i].display), "%s",
             cms->elems[i].display == NULL ? "" : cms->elems[i].display);
    results[i].before = cms->elems[i].delete_before;
    results[i].after = cms->elems[i].delete_after;
  }
  qsort(results, (size_t)(n > 0 ? n : 0), sizeof(result_t), &compare_results);

  printf("files ");
  print_str(prefix);
  printf(" %02x ", (unsigned char)dirsep);
  print_str(roots); printf(" "); print_str(extensions);
  printf(" %d %zd", no_lscolor ? 1 : 0, n);
  for (ssize_t i = 0; i < n; i++) {
    printf(" ");
    print_str(results[i].replacement);
    printf(":");
    print_str(results[i].display);
    printf(":%zd:%zd", results[i].before, results[i].after);
  }
  printf("\n");
  free(results);
  completions_free(cms);
}

/* ------------------------------------------------------------------------
   The tree

   Everything here can be made without special privileges. A block or
   character device cannot, so those two file types are not covered.
   ------------------------------------------------------------------------ */
/* Remove exactly what make_tree creates, so that a run is not affected by
   what an earlier one left behind. Removing a known list is safer than
   emptying a directory whose contents are not known. */
static void remove_tree(const char* root) {
  char path[1024];
  static const char* entries[] = {
    "banana/inner.txt", "apple.txt", "apricot.md", "cherry.TXT", "date.c",
    "UPPER.txt", ".hidden", "no-ext", "exec.sh", "link", NULL
  };
  for (int i = 0; entries[i] != NULL; i++) {
    snprintf(path, sizeof(path), "%s/%s", root, entries[i]);
    remove(path);
  }
  static const char* dirs[] = { "banana", "sub dir", "sticky", NULL };
  for (int i = 0; dirs[i] != NULL; i++) {
    snprintf(path, sizeof(path), "%s/%s", root, dirs[i]);
    rmdir(path);
  }
  rmdir(root);
}

static void make_tree(const char* root) {
  char path[1024];
  mkdir(root, 0755);
  #define AT(name) (snprintf(path, sizeof(path), "%s/%s", root, name), path)
  FILE* f;
  const char* files[] = {
    "apple.txt", "apricot.md", "cherry.TXT", "date.c", "UPPER.txt", ".hidden",
    "no-ext", NULL
  };
  for (int i = 0; files[i] != NULL; i++) {
    f = fopen(AT(files[i]), "w");
    if (f != NULL) { fputs("x", f); fclose(f); }
  }
  f = fopen(AT("exec.sh"), "w");
  if (f != NULL) { fputs("#!/bin/sh\n", f); fclose(f); }
  chmod(AT("exec.sh"), 0755);

  mkdir(AT("banana"), 0755);
  mkdir(AT("sub dir"), 0755);
  mkdir(AT("sticky"), 01755);

  snprintf(path, sizeof(path), "%s/banana/inner.txt", root);
  f = fopen(path, "w");
  if (f != NULL) { fputs("x", f); fclose(f); }

  snprintf(path, sizeof(path), "%s/link", root);
  symlink("apple.txt", path);
  #undef AT
}

int main(void) {
  /* Extensions, which need no file system at all. */
  static const char* names[] = {
    "file.txt", "file.TXT", "file.c", "file", "a.tar.gz", ".txt", "", "x.txt.bak"
  };
  static const char* exts[] = {
    "", ".txt", ".txt;.md", ".c;.h", ".gz", ".TXT", ";", ".txt;", ";.txt", "txt"
  };
  for (int i = 0; i < (int)(sizeof(names)/sizeof(names[0])); i++) {
    for (int j = 0; j < (int)(sizeof(exts)/sizeof(exts[0])); j++) {
      extension_case(names[i], exts[j]);
    }
  }

  /* Colouring, over both styles and every file type. */
  static const char* clicolors[] = { NULL, "1", "", "0", "yes" };
  for (int c = 0; c < (int)(sizeof(clicolors)/sizeof(clicolors[0])); c++) {
    for (int ft = 0; ft < FT_LAST; ft++) {
      colorize_case(clicolors[c], NULL, NULL, ft, "name", NULL, 0, false);
      colorize_case(clicolors[c], NULL, "ExFxCxDxBxegedabagacad", ft, "name", NULL, '/', false);
      colorize_case(clicolors[c], "di=01;34:ln=01;36:ex=01;32:*.txt=00;33", NULL,
                    ft, "name", ".txt", 0, false);
      colorize_case(clicolors[c], "di=01;34", NULL, ft, "name", ".md", 0, false);
      colorize_case(clicolors[c], NULL, NULL, ft, "name", NULL, 0, true);
    }
  }
  /* A key that is there but empty, and one that is missing. */
  colorize_case("1", "di=:ln=01;36", NULL, FT_DIR, "name", NULL, 0, false);
  colorize_case("1", "ln=01;36", NULL, FT_DIR, "name", NULL, 0, false);
  colorize_case("1", "*.txt=33", NULL, FT_DEFAULT, "name", ".txt", 0, false);
  colorize_case("1", "*.txt=33", NULL, FT_DEFAULT, "name", ".md", 0, false);
  colorize_case("1", "", "", FT_DIR, "name", NULL, 0, false);

  /* The file system cases. */
  /* A fixed name rather than mkdtemp, which the feature test macros above
     hide on macOS. The tree is removed first, so a run does not see what an
     earlier one left. */
  const char* root = "/tmp/rline-probe-filenames";
  remove_tree(root);
  make_tree(root);
  if (chdir(root) != 0) { fprintf(stderr, "cannot change directory\n"); return 1; }

  static const char* prefixes[] = {
    "", "a", "ap", "app", "b", "ban", "banana/", "banana/i", "c", "d", "e",
    "l", "s", "sub", "st", "U", "u", "UPPER", "upper", ".h", "x", "no",
    "./a", "sub\\ dir/", "'sub dir/", "\"sub dir/",
  };
  for (int i = 0; i < (int)(sizeof(prefixes)/sizeof(prefixes[0])); i++) {
    files_case(prefixes[i], '/', ".", "", false);
    files_case(prefixes[i], 0, ".", "", false);
    files_case(prefixes[i], '/', ".", ".txt", false);
    files_case(prefixes[i], '/', ".", ".txt;.md", false);
    files_case(prefixes[i], '/', ".;banana", "", false);
  }
  return 0;
}
