#!/usr/bin/env python3
"""Prints the source of the uncovered blocks of one file (from coverage.out)."""
import re, sys
want = sys.argv[1]
blocks = {}
for l in open('coverage.out').read().split('\n')[1:]:
    m = re.match(r'(.+?):(\d+)\.\d+,(\d+)\.\d+ (\d+) (\d+)', l)
    if not m or not m[1].endswith(want):
        continue
    k = (int(m[2]), int(m[3]))
    blocks[k] = max(blocks.get(k, 0), int(m[5]))
src = open([p for p in __import__('glob').glob('internal/**/*.go', recursive=True) + __import__('glob').glob('cmd/**/*.go', recursive=True) if p.endswith(want)][0]).read().split('\n')
last = 0
for (s, e), c in sorted(blocks.items()):
    if c == 0:
        if s - last > 1:
            print('   ...')
        for i in range(s, e + 1):
            print(f"{i:4} {src[i-1]}")
        last = e
