#!/usr/bin/env python3
"""Lists uncovered code from coverage.out, by file and block (merged across packages)."""
import re, sys, collections
blocks = {}
for l in open('coverage.out').read().split('\n')[1:]:
    if not l:
        continue
    m = re.match(r'(.+?):(\d+)\.(\d+),(\d+)\.(\d+) (\d+) (\d+)', l)
    f, s, sc, e, ec, n, c = m.groups()
    k = (f.replace('github.com/oscarmlage/sltty/', ''), int(s), int(e))
    prev = blocks.get(k, (int(n), 0))
    blocks[k] = (int(n), max(prev[1], int(c)))
tot = sum(n for n, _ in blocks.values())
miss = collections.Counter()
per = collections.defaultdict(list)
for (f, s, e), (n, c) in blocks.items():
    if c == 0 and 'names_gen' not in f:
        miss[f] += n
        per[f].append((s, e, n))
print(f"statements {tot}, uncovered {sum(miss.values())}, coverage {100 - 100 * sum(miss.values()) / tot:.1f}%, allowed for 95%: {int(tot * 0.05)}")
only = sys.argv[1] if len(sys.argv) > 1 else None
for f, n in miss.most_common():
    if only and only not in f:
        continue
    print(n, f)
    if only:
        for s, e, k in sorted(per[f]):
            print(f"   {s}-{e} ({k})")
