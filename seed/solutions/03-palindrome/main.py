import sys

s = sys.stdin.buffer.readline().strip()
print("YES" if s == s[::-1] else "NO")
