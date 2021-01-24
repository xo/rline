#!/bin/bash

set -e

SRC=$(realpath $(cd -P "$(dirname "${BASH_SOURCE[0]}")" && pwd))

READLINE_VERSION=8.1
REPLXX_VERSION=0.0.3

READLINE_ARCHIVE=https://ftp.gnu.org/gnu/readline/readline-$READLINE_VERSION.tar.gz
REPLXX_ARCHIVE=https://github.com/AmokHuginnsson/replxx/archive/release-$REPLXX_VERSION.tar.gz

grab() {
  echo -n "RETRIEVING: $1 -> $2     "
  wget --progress=dot -O $2 $1 2>&1 |\
    grep --line-buffered "%" | \
    sed -u -e "s,\.,,g" | \
    awk '{printf("\b\b\b\b%4s", $2)}'
  echo -ne "\b\b\b\b"
  echo " DONE."
}

cache() {
  FILE=$(basename $2)
  if [ ! -z "$3" ]; then
    FILE="$3"
  fi
  if [ ! -f $1/$FILE ]; then
    grab $2 $1/$FILE
  fi
}

# retrieve and build readline
cache $SRC $READLINE_ARCHIVE
WORKDIR=$SRC/readline-$READLINE_VERSION
if [ ! -d $WORKDIR ]; then
  pushd $SRC &> /dev/null
  tar -zxf $(basename $READLINE_ARCHIVE)
  popd &> /dev/null
fi
pushd $WORKDIR &> /dev/null
(set -x;
  CFLAGS='-fPIC' \
  ./configure \
    --enable-static \
    --disable-shared \
    --without-curses
  make -j $((`nproc`+2))
)
mkdir -p $SRC/readline
rm -f $SRC/readline/*
(set -x;
  cp -a $SRC/readline-$READLINE_VERSION/*.{a,h} $SRC/readline
)
popd &> /dev/null

# retrieve and build replxx
cache $SRC $REPLXX_ARCHIVE replxx-$REPLXX_VERSION.tar.gz
WORKDIR=$SRC/replxx-$REPLXX_VERSION
if [ ! -d $WORKDIR ]; then
  pushd $SRC &> /dev/null
  tar -zxf replxx-$REPLXX_VERSION.tar.gz
  mv replxx-release-$REPLXX_VERSION replxx-$REPLXX_VERSION
  popd &> /dev/null
fi
mkdir -p $WORKDIR/build
pushd $WORKDIR/build &> /dev/null
(set -x;
  cmake -DCMAKE_POSITION_INDEPENDENT_CODE=ON ..
  make -j $((`nproc`+2))
)
mkdir -p $SRC/replxx
rm -f $SRC/replxx/*
(set -x;
  cp -a $SRC/replxx-$REPLXX_VERSION/build/*.a $SRC/replxx
  cp -a $SRC/replxx-$REPLXX_VERSION/include/*.h $SRC/replxx
)
popd &> /dev/null
