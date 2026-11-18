#include <iostream>
#include <string>
using namespace std;
int main() {
    ios::sync_with_stdio(false);
    cin.tie(nullptr);
    string s, stack;
    getline(cin, s);
    bool ok = true;
    for (char c : s) {
        if (c == '\r') continue;
        if (c == '(' || c == '[' || c == '{') stack.push_back(c);
        else {
            char expected = c == ')' ? '(' : c == ']' ? '[' : '{';
            if (stack.empty() || stack.back() != expected) { ok = false; break; }
            stack.pop_back();
        }
    }
    cout << (ok && stack.empty() ? "YES" : "NO") << '\n';
}
