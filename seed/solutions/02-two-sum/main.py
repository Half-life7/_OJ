import sys

data = list(map(int, sys.stdin.buffer.read().split()))
n, target = data[:2]
seen = {}
for i, x in enumerate(data[2:2 + n], 1):
    if target - x in seen:
        print(seen[target - x], i)
        break
    seen.setdefault(x, i)
