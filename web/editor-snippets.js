// $0 marks the cursor position after expanding a snippet.
export const snippets = {
  cpp: [
    ['cin', '读入变量', 'cin >> $0;'],
    ['cout', '输出并换行', "cout << $0 << '\\n';"],
    ['for', '计数循环', 'for (int i = 0; i < n; ++i) {\n    $0\n}'],
    ['while', '条件循环', 'while ($0) {\n    \n}'],
    ['ios', '快速输入输出', 'ios::sync_with_stdio(false);\ncin.tie(nullptr);\n$0'],
    ['vector', '动态数组', 'vector<int> $0;'],
    ['sort', '排序区间', 'sort(v.begin(), v.end());$0'],
    ['main', '主函数', 'int main() {\n    $0\n    return 0;\n}'],
  ],
  python: [
    ['input', '读取一行', 'input($0)'],
    ['print', '输出内容', 'print($0)'],
    ['for', '计数循环', 'for i in range(n):\n    $0'],
    ['while', '条件循环', 'while $0:\n    '],
    ['ints', '读取一行整数', 'list(map(int, input().split()))$0'],
    ['def', '定义函数', 'def $0():\n    pass'],
    ['main', '主程序入口', 'if __name__ == "__main__":\n    $0'],
  ],
  java: [
    ['sout', 'System.out.println', 'System.out.println($0);'],
    ['Scanner', '读取标准输入', 'java.util.Scanner sc = new java.util.Scanner(System.in);\n$0'],
    ['for', '计数循环', 'for (int i = 0; i < n; i++) {\n    $0\n}'],
    ['while', '条件循环', 'while ($0) {\n    \n}'],
    ['main', '主方法', 'public static void main(String[] args) {\n    $0\n}'],
  ],
};
