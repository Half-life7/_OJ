#include <iostream>
#include <unordered_map>
using namespace std;
int main() {
    ios::sync_with_stdio(false);
    cin.tie(nullptr);
    int n;
    long long target;
    if (!(cin >> n >> target)) return 0;
    unordered_map<long long, int> seen;
    seen.reserve(n * 2);
    for (int i = 1; i <= n; ++i) {
        long long x;
        cin >> x;
        auto it = seen.find(target - x);
        if (it != seen.end()) {
            cout << it->second << ' ' << i << '\n';
            return 0;
        }
        seen.emplace(x, i);
    }
}
