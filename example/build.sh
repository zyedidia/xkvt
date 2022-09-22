gcc -Wall -O2 -c foo.c -o foo.o
gcc -Wall -O2 -c bar.c -o bar.o
gcc -Wall -O2 bar.o foo.o -o prog
