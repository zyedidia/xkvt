# eXKaVaTe

Xkvt is a tool that runs a series of commands and automatically determines the input/output files that are consumed/produced by each command. It can then output a build file for Make or [Knit](https://github.com/zyedidia/knit), or a generic JSON representation.

Xkvt uses ptrace to determine what files are read and written by each command.

Xkvt only supports Linux AMD64.

# Example

Suppose you have `foo.c`, `bar.c` and `bar.h` (see `example/`). The program could be built by running

```
gcc -Wall -O2 -c foo.c -o foo.o
gcc -Wall -O2 -c bar.c -o bar.o
gcc -Wall -O2 bar.o foo.o -o prog
```

These commands are in `build.sh`. If you run these commands through Xkvt, it can automatically produce a Makefile for this build:

```
$ xkvt -i build.sh -f make -o Makefile
gcc -Wall -O2 -c foo.c -o foo.o
gcc -Wall -O2 -c bar.c -o bar.o
gcc -Wall -O2 bar.o foo.o -o prog
$ cat Makefile
foo.o: foo.c bar.h
	gcc -Wall -O2 -c foo.c -o foo.o
bar.o: bar.c
	gcc -Wall -O2 -c bar.c -o bar.o
prog: bar.o foo.o
	gcc -Wall -O2 bar.o foo.o -o prog
```

Note that Xkvt automatically detects that `foo.o` depends on `bar.h` (as well as `foo.c`).

Xkvt can also output a Knitfile, or a generic JSON representation that looks like this:

<details>
  <summary>Show JSON</summary>

    {
      "Commands": [
        {
          "Command": "gcc -Wall -O2 -c foo.c -o foo.o",
          "Inputs": [
            "foo.c",
            "bar.h"
          ],
          "Outputs": [
            "foo.o"
          ]
        },
        {
          "Command": "gcc -Wall -O2 -c bar.c -o bar.o",
          "Inputs": [
            "bar.c"
          ],
          "Outputs": [
            "bar.o"
          ]
        },
        {
          "Command": "gcc -Wall -O2 bar.o foo.o -o prog",
          "Inputs": [
            "bar.o",
            "foo.o"
          ],
          "Outputs": [
            "prog"
          ]
        }
      ]
    }

</details>

# Usage

```
Usage of xkvt:
  -f, --format string   output format (default "json")
  -h, --help            show this help message
  -i, --input string    input file
  -o, --output string   output file
  -V, --verbose         verbose debugging information
```
