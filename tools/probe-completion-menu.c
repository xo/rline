/* Print what isocline's completion menu draws and what it leaves behind, so
   that the Go port can be checked against it.

   The menu is the one part of the editor that reads its own keys, so it
   cannot be driven from outside: it is called once per case with the keys
   for that case already loaded into the tty. It then draws, reads, moves the
   selection, and returns when a key ends it.

   The terminal sits on a real pseudo-terminal so the drawing goes somewhere
   real, the same way tools/probe-refresh.c does it. The tty is built by hand
   on the read end of an empty pipe, the same way tools/probe-tty.c does it,
   because tty_new refuses anything that is not a terminal and because a
   terminal with nothing pending is the only thing that stands in correctly
   for nobody typing.

   Two keys are deliberately not in any case. F1 opens the help screen, whose
   text differs between macOS and everywhere else, and putting it here would
   drag that split into this corpus for no gain: the help has its own. And
   page-down with more completions available asks the completer for the rest,
   and there is no completer here, so only the branch that shows what is
   already known is reached.

   The output is a golden file. */
#define _XOPEN_SOURCE 700
#define _DEFAULT_SOURCE
#if defined(__APPLE__)
/* cfmakeraw is a BSD extension, and asking for _XOPEN_SOURCE hides it on
   macOS. This asks for it back. */
#define _DARWIN_C_SOURCE
#endif

#include "isocline.c"

#include <stdio.h>
#include <string.h>
#include <stdlib.h>
#include <fcntl.h>
#include <poll.h>
#include <pthread.h>
#include <errno.h>
#include <time.h>

static alloc_t probe_mem = { &malloc, &realloc, &free };
static int leader_fd = -1, follower_fd = -1, idle_fd = -1;

static int open_pty(int cols, int rows) {
  leader_fd = posix_openpt(O_RDWR | O_NOCTTY);
  if (leader_fd < 0) return -1;
  if (grantpt(leader_fd) < 0 || unlockpt(leader_fd) < 0) return -1;
  char* name = ptsname(leader_fd);
  if (name == NULL) return -1;
  follower_fd = open(name, O_RDWR | O_NOCTTY);
  if (follower_fd < 0) return -1;
  struct termios tio;
  if (tcgetattr(follower_fd, &tio) == 0) { cfmakeraw(&tio); tcsetattr(follower_fd, TCSANOW, &tio); }
  struct winsize ws;
  memset(&ws, 0, sizeof(ws));
  ws.ws_col = (unsigned short)cols; ws.ws_row = (unsigned short)rows;
  if (ioctl(follower_fd, TIOCSWINSZ, &ws) < 0) return -1;
  return 0;
}

static void print_esc(const char* s, ssize_t n) {
  if (n <= 0) { printf("-"); return; }
  for (ssize_t i = 0; i < n; i++) {
    unsigned char c = (unsigned char)s[i];
    if (c == 0x1b) printf("\\e");
    else if (c == '\\') printf("\\\\");
    else if (c == '\r') printf("\\r");
    else if (c == '\n') printf("\\n");
    else if (c == ' ') printf("\\s");
    else if (c < 0x20 || c >= 0x7f) printf("\\x%02x", c);
    else printf("%c", c);
  }
}

/* What has been drawn since the last emit.

   A thread does the reading rather than emit itself, because the menu draws
   without ever giving the probe a chance to read: it fills the pseudo
   terminal, the write inside term_flush blocks waiting for someone to take
   the bytes, and the probe stops for good. That is not hypothetical. An
   earlier version read only between cases and ran to the end for as long as
   the drawing had no color in it; defining the styles made every row longer
   and it hung part way through, at the widest set of completions. */
#define CAPMAX (1 << 20)
static char capbuf[CAPMAX];
static size_t caplen = 0;
static pthread_mutex_t caplock = PTHREAD_MUTEX_INITIALIZER;

static void* capture_thread(void* arg) {
  (void)arg;
  char buf[4096];
  for (;;) {
    ssize_t n = read(leader_fd, buf, sizeof(buf));
    if (n < 0) { if (errno == EINTR) continue; break; }
    if (n == 0) break;
    pthread_mutex_lock(&caplock);
    if (caplen + (size_t)n <= CAPMAX) { memcpy(capbuf + caplen, buf, (size_t)n); caplen += (size_t)n; }
    pthread_mutex_unlock(&caplock);
  }
  return NULL;
}

/* Print everything drawn since the last call, and forget it.

   The caller has already flushed, so the bytes are in the pseudo terminal
   and the thread will have them shortly. Wait until the count stops growing
   rather than for a fixed time, so a large case is not cut short. */
static void emit(void) {
  size_t seen = (size_t)-1;
  for (;;) {
    struct timespec ts = { 0, 10 * 1000 * 1000 };
    nanosleep(&ts, NULL);
    pthread_mutex_lock(&caplock);
    size_t now = caplen;
    pthread_mutex_unlock(&caplock);
    if (now == seen) break;
    seen = now;
  }
  pthread_mutex_lock(&caplock);
  print_esc(capbuf, (ssize_t)caplen);
  caplen = 0;
  pthread_mutex_unlock(&caplock);
}

static ic_env_t env;
static editor_t eb;
static tty_t probe_tty;

/* Load the bytes a case types. tty_cpop takes from the end, so they go in
   backwards and byte 0 comes out first. */
static void tty_reset(const char* keys, bool utf8) {
  ssize_t n = (ssize_t)strlen(keys);
  if (n > TTY_PUSH_MAX) { fprintf(stderr, "case too long\n"); exit(1); }
  memset(&probe_tty, 0, sizeof(probe_tty));
  probe_tty.mem = &probe_mem;
  probe_tty.fd_in = idle_fd;
  probe_tty.is_utf8 = utf8;
  probe_tty.esc_initial_timeout = 0;
  probe_tty.esc_timeout = 0;
  for (ssize_t i = 0; i < n; i++) {
    probe_tty.cpushbuf[i] = (uint8_t)keys[n - i - 1];
  }
  probe_tty.cpush_count = n;
}

/* The sets of completions the menu is shown for. Each entry is a
   replacement, a display and a help, with an empty display or help meaning
   there is none. */
typedef struct { const char* rep; const char* disp; const char* help; } entry_t;

static const entry_t set_two[] = {
  {"alpha", "", ""}, {"alpine", "", ""}
};
/* Exactly three, and narrow. The three column branch asks for more than
   three, so this is what pins that it does. */
static const entry_t set_three[] = {
  {"ta", "", ""}, {"tb", "", ""}, {"tc", "", ""}
};
static const entry_t set_five[] = {
  {"aa", "", ""}, {"ab", "", ""}, {"ac", "", ""}, {"ad", "", ""}, {"ae", "", ""}
};
static const entry_t set_nine[] = {
  {"a1", "", ""}, {"a2", "", ""}, {"a3", "", ""}, {"a4", "", ""}, {"a5", "", ""},
  {"a6", "", ""}, {"a7", "", ""}, {"a8", "", ""}, {"a9", "", ""}
};
static const entry_t set_twelve[] = {
  {"b01", "", ""}, {"b02", "", ""}, {"b03", "", ""}, {"b04", "", ""},
  {"b05", "", ""}, {"b06", "", ""}, {"b07", "", ""}, {"b08", "", ""},
  {"b09", "", ""}, {"b10", "", ""}, {"b11", "", ""}, {"b12", "", ""}
};
/* Wide enough that three columns will not fit, and neither will two, so this
   either, so this falls to the list. */
/* Nine wide, which at this terminal width is the one set that sits on the
   boundary between three columns and two: three need 3*(3+9)+2*2 = 40 and
   the width to beat is 40, so they do not fit and two do. It is here because
   it is the only thing that can tell the two spaces between three columns
   from one, or the one column the menu keeps in hand from none. Neither is
   reachable at a width of forty, where the arithmetic cannot land on the
   boundary at all. */
static const entry_t set_w9[] = {
  {"g23456789", "", ""}, {"h23456789", "", ""}, {"i23456789", "", ""},
  {"j23456789", "", ""}, {"k23456789", "", ""}
};
/* Too wide for any column layout, and more than the list shows. */
static const entry_t set_wide10[] = {
  {"aaaaaaaaaaaaaaaaaaaaaaaaaaaa00", "", ""},
  {"aaaaaaaaaaaaaaaaaaaaaaaaaaaa01", "", ""},
  {"aaaaaaaaaaaaaaaaaaaaaaaaaaaa02", "", ""},
  {"aaaaaaaaaaaaaaaaaaaaaaaaaaaa03", "", ""},
  {"aaaaaaaaaaaaaaaaaaaaaaaaaaaa04", "", ""},
  {"aaaaaaaaaaaaaaaaaaaaaaaaaaaa05", "", ""},
  {"aaaaaaaaaaaaaaaaaaaaaaaaaaaa06", "", ""},
  {"aaaaaaaaaaaaaaaaaaaaaaaaaaaa07", "", ""},
  {"aaaaaaaaaaaaaaaaaaaaaaaaaaaa08", "", ""},
  {"aaaaaaaaaaaaaaaaaaaaaaaaaaaa09", "", ""}
};
/* The first of these is what the line already reads, so applying it moves
   neither the line nor the cursor. That is the one case the menu redraws for
   even though nothing changed, because the selection still has to move. */
static const entry_t set_noop[] = {
  {"a", "", ""}, {"ab", "", ""}, {"ac", "", ""}, {"ad", "", ""}, {"ae", "", ""}
};
static const entry_t set_wide[] = {
  {"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "", ""},
  {"aaaaaaaaaaaaaaaaaaaaaaaaaaaaab", "", ""},
  {"aaaaaaaaaaaaaaaaaaaaaaaaaaaaac", "", ""},
  {"aaaaaaaaaaaaaaaaaaaaaaaaaaaaad", "", ""},
  {"aaaaaaaaaaaaaaaaaaaaaaaaaaaaae", "", ""}
};
/* Middling width: too wide for three columns, narrow enough for
   two. Three columns need 3*(3+w)+4 < 40 and two need 2*(3+w)+2 < 40, so a
   width of twelve reaches the two column branch. */
static const entry_t set_mid[] = {
  {"cccccccccccc", "", ""}, {"ccccccccccca", "", ""}, {"cccccccccccb", "", ""},
  {"cccccccccccd", "", ""}, {"ccccccccccce", "", ""}, {"cccccccccccf", "", ""},
  {"cccccccccccg", "", ""}
};
/* Four of that width, which the two column branch asks for more than, and
   ten, which is more than it shows. */
static const entry_t set_mid4[] = {
  {"eeeeeeeeeeee", "", ""}, {"eeeeeeeeeeea", "", ""},
  {"eeeeeeeeeeeb", "", ""}, {"eeeeeeeeeeec", "", ""}
};
static const entry_t set_mid10[] = {
  {"ffffffffff00", "", ""}, {"ffffffffff01", "", ""}, {"ffffffffff02", "", ""},
  {"ffffffffff03", "", ""}, {"ffffffffff04", "", ""}, {"ffffffffff05", "", ""},
  {"ffffffffff06", "", ""}, {"ffffffffff07", "", ""}, {"ffffffffff08", "", ""},
  {"ffffffffff09", "", ""}
};
/* The same width, but only six of them, which is the other row count the two
   column branch can choose. */
static const entry_t set_mid6[] = {
  {"dddddddddddd", "", ""}, {"ddddddddddda", "", ""}, {"dddddddddddb", "", ""},
  {"dddddddddddc", "", ""}, {"ddddddddddde", "", ""}, {"dddddddddddf", "", ""}
};
/* With a display and a help of their own. */
static const entry_t set_help[] = {
  {"one", "[ic-emphasis]one[/]", "the first"},
  {"two", "two", "the second"},
  {"three", "", "the third"}
};
/* Entries outside ASCII, so the column width is measured rather than counted. */
static const entry_t set_utf8[] = {
  {"\xe6\x97\xa5\xe6\x9c\xac", "", ""},
  {"\xe6\x97\xa5\xe6\x9b\x9c", "", ""},
  {"\xc3\xa9t\xc3\xa9", "", ""},
  {"na\xc3\xafve", "", ""},
  {"stra\xc3\x9f\x65", "", ""}
};

typedef struct { const char* name; const entry_t* items; int count; } set_t;
static const set_t sets[] = {
  {"two",    set_two,    (int)(sizeof(set_two)/sizeof(entry_t))},
  {"three",  set_three,  (int)(sizeof(set_three)/sizeof(entry_t))},
  {"five",   set_five,   (int)(sizeof(set_five)/sizeof(entry_t))},
  {"nine",   set_nine,   (int)(sizeof(set_nine)/sizeof(entry_t))},
  {"twelve", set_twelve, (int)(sizeof(set_twelve)/sizeof(entry_t))},
  {"w9",     set_w9,     (int)(sizeof(set_w9)/sizeof(entry_t))},
  {"wide",   set_wide,   (int)(sizeof(set_wide)/sizeof(entry_t))},
  {"wide10", set_wide10, (int)(sizeof(set_wide10)/sizeof(entry_t))},
  {"noop",   set_noop,   (int)(sizeof(set_noop)/sizeof(entry_t))},
  {"mid",    set_mid,    (int)(sizeof(set_mid)/sizeof(entry_t))},
  {"mid4",   set_mid4,   (int)(sizeof(set_mid4)/sizeof(entry_t))},
  {"mid6",   set_mid6,   (int)(sizeof(set_mid6)/sizeof(entry_t))},
  {"mid10",  set_mid10,  (int)(sizeof(set_mid10)/sizeof(entry_t))},
  {"help",   set_help,   (int)(sizeof(set_help)/sizeof(entry_t))},
  {"utf8",   set_utf8,   (int)(sizeof(set_utf8)/sizeof(entry_t))}
};

/* The keys each case types, as the bytes a terminal would send. */
/* What the completer offers when the menu asks for the rest of them. A
   completer has to be set, because completions_new leaves the default
   filename completer in place and that would make the recording depend on
   which directory it was made in: the first attempt at this corpus listed the
   Go sources of this package. */
static const char* generated[] = {
  "gen01", "gen02", "gen03", "gen04", "gen05", "gen06",
  "gen07", "gen08", "gen09", "gen10", "gen11", "gen12"
};

static void probe_completer(ic_completion_env_t* cenv, const char* prefix) {
  (void)prefix;
  for (int i = 0; i < (int)(sizeof(generated)/sizeof(generated[0])); i++) {
    if (!ic_add_completion(cenv, generated[i])) return;
  }
}

typedef struct { const char* name; const char* keys; } script_t;
static const script_t scripts[] = {
  {"esc",        "\x1b"},
  {"enter",      "\r"},
  {"down-enter", "\x1b[B\r"},
  {"down2-enter","\x1b[B\x1b[B\r"},
  {"up-enter",   "\x1b[A\r"},
  {"tab-enter",  "\t\r"},
  {"digit3",     "3"},
  {"digit9",     "9"},
  {"right",      "\x1b[C"},
  {"end",        "\x1b[F"},
  {"letter",     "x"},
  {"pagedown",   "\x1b[6~"},
  {"ctrl-j",     "\n"},
  {"home",       "\x1b[H"}
};

int main(void) {
  int fds[2];
  if (pipe(fds) != 0) { fprintf(stderr, "no pipe\n"); return 1; }
  idle_fd = fds[0];
  /* tty_readc_noblock asks FIONREAD about file descriptor 0 rather than its
     own, so the same idle pipe goes there too. */
  if (dup2(idle_fd, 0) < 0) { fprintf(stderr, "no dup2\n"); return 1; }
  if (open_pty(41, 6) < 0) { fprintf(stderr, "no pty\n"); return 1; }
  pthread_t capturer;
  if (pthread_create(&capturer, NULL, &capture_thread, NULL) != 0) {
    fprintf(stderr, "no capture thread\n"); return 1;
  }
  pthread_detach(capturer);
  setenv("TERM", "xterm-256color", 1);
  unsetenv("COLORTERM"); unsetenv("NO_COLOR"); unsetenv("COLUMNS"); unsetenv("LINES");

  memset(&env, 0, sizeof(env));
  env.mem = &probe_mem;
  env.term = term_new(&probe_mem, NULL, false, true, follower_fd);
  env.tty = &probe_tty;
  env.bbcode = bbcode_new(&probe_mem, env.term);
  /* The styles ic_env_new defines. Without them every style name in the menu
     renders to nothing, and the recording cannot tell one name from another:
     an earlier version of this probe drew the help text of a completion in
     no color at all and would have passed with the wrong style on it. */
  bbcode_style_def(env.bbcode, "ic-prompt",    "ansi-green");
  bbcode_style_def(env.bbcode, "ic-info",      "ansi-darkgray");
  bbcode_style_def(env.bbcode, "ic-diminish",  "ansi-lightgray");
  bbcode_style_def(env.bbcode, "ic-emphasis",  "#ffffd7");
  bbcode_style_def(env.bbcode, "ic-hint",      "ansi-darkgray");
  bbcode_style_def(env.bbcode, "ic-error",     "#d70000");
  bbcode_style_def(env.bbcode, "ic-bracematch","ansi-white");
  bbcode_style_def(env.bbcode, "keyword",  "#569cd6");
  bbcode_style_def(env.bbcode, "control",  "#c586c0");
  bbcode_style_def(env.bbcode, "number",   "#b5cea8");
  bbcode_style_def(env.bbcode, "string",   "#ce9178");
  bbcode_style_def(env.bbcode, "comment",  "#6A9955");
  bbcode_style_def(env.bbcode, "type",     "darkcyan");
  bbcode_style_def(env.bbcode, "constant", "#569cd6");
  env.completions = completions_new(&probe_mem);
  completions_set_completer(env.completions, &probe_completer, NULL);
  env.prompt_marker = "> ";
  env.cprompt_marker = "| ";
  env.match_braces = "()[]{}";
  env.auto_braces = "()[]{}";
  env.multiline_eol = '\\';
  env.no_highlight = true;

  memset(&eb, 0, sizeof(eb));
  eb.input = sbuf_new(&probe_mem);
  eb.extra = sbuf_new(&probe_mem);
  eb.hint = sbuf_new(&probe_mem);
  eb.hint_help = sbuf_new(&probe_mem);
  eb.attrs = attrbuf_new(&probe_mem);
  eb.attrs_extra = attrbuf_new(&probe_mem);
  eb.mem = &probe_mem;

  int caseno = 0;
  for (int s = 0; s < (int)(sizeof(sets)/sizeof(sets[0])); s++) {
    for (int k = 0; k < (int)(sizeof(scripts)/sizeof(scripts[0])); k++) {
      for (int more = 0; more < 2; more++) {
        for (int nopreview = 0; nopreview < 2; nopreview++) {
          for (int autotab = 0; autotab < 2; autotab++) {
            for (int utf8 = 0; utf8 < 2; utf8++) {
              completions_clear(env.completions);
              /* completions_add refuses to add anything once the budget the
                 completer was given runs out, and the budget is normally set
                 by completions_generate. Nothing runs a completer here, so
                 the budget is set by hand. */
              ((struct completions_s*)env.completions)->completer_max = IC_MAX_COMPLETIONS_TO_SHOW;
              for (int i = 0; i < sets[s].count; i++) {
                const entry_t* it = &sets[s].items[i];
                completions_add(env.completions, it->rep,
                                (it->disp[0] == 0 ? NULL : it->disp),
                                (it->help[0] == 0 ? NULL : it->help), 1, 0);
              }
              sbuf_replace(eb.input, "a");
              sbuf_clear(eb.extra);
              sbuf_clear(eb.hint);
              sbuf_clear(eb.hint_help);
              eb.pos = 1;
              eb.termw = 41;
              eb.cur_rows = 1;
              eb.cur_row = 0;
              eb.prompt_text = "p";
              eb.modified = false;
              eb.disable_undo = false;
              editstate_done(&probe_mem, &eb.undo);
              editstate_done(&probe_mem, &eb.redo);
              editstate_init(&eb.undo);
              editstate_init(&eb.redo);
              env.complete_nopreview = (nopreview != 0);
              env.complete_autotab = (autotab != 0);
              tty_reset(scripts[k].keys, utf8 != 0);

              term_flush(env.term);
              emit(); /* discard anything pending */
              printf("\n");

              edit_completion_menu(&env, &eb, more != 0);
              term_flush(env.term);

              printf("menu %d %s %s more=%d nopreview=%d autotab=%d utf8=%d ",
                     caseno++, sets[s].name, scripts[k].name,
                     more, nopreview, autotab, utf8);
              emit();
              printf(" pos=%d input=", (int)eb.pos);
              print_esc(sbuf_string(eb.input), sbuf_len(eb.input));
              printf(" left=%d pushed=", (int)completions_count(env.completions));
              if (probe_tty.push_count == 0) { printf("-"); }
              for (ssize_t i = 0; i < probe_tty.push_count; i++) {
                printf("%08x", (unsigned)probe_tty.pushbuf[i]);
              }
              printf("\n");
            }
          }
        }
      }
    }
  }
  return 0;
}
