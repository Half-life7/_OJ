import java.io.BufferedInputStream;
import java.util.HashMap;
import java.util.Map;

public class Main {
    private static final BufferedInputStream in = new BufferedInputStream(System.in);
    private static long nextLong() throws Exception {
        int c;
        do { c = in.read(); } while (c >= 0 && c <= 32);
        boolean negative = c == '-';
        if (negative) c = in.read();
        long value = 0;
        while (c > 32 && c >= 0) {
            value = value * 10 + c - '0';
            c = in.read();
        }
        return negative ? -value : value;
    }
    public static void main(String[] args) throws Exception {
        int n = (int) nextLong();
        long target = nextLong();
        Map<Long, Integer> seen = new HashMap<>();
        for (int i = 1; i <= n; i++) {
            long value = nextLong();
            Integer left = seen.get(target - value);
            if (left != null) {
                System.out.println(left + " " + i);
                return;
            }
            seen.putIfAbsent(value, i);
        }
    }
}
