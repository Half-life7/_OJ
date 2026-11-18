#include <iostream>
#include <string>
using namespace std;
int main() {
    ios::sync_with_stdio(false);
    cin.tie(nullptr);
    string s;
    cin >> s;
    bool ok = true;
    for (size_t i = 0; i < s.size() / 2; ++i)
        if (s[i] != s[s.size() - 1 - i]) ok = false;
    cout << (ok ? "YES" : "NO") << '\n';
}
