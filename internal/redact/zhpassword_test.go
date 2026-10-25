package redact

import (
	"strings"
	"testing"
)

// A password written after its name in Chinese, Japanese or Korean is
// masked, a short one and one of digits only too: 密码：786kih went to the
// vendor as written (CyberCaius on Discord). The name, the words around it
// and a sentence's closing stop stay.
func TestCJKPasswordMasked(t *testing.T) {
	cases := []struct {
		in, gone string
		kept     []string
	}{
		{"密码：786kih", "786kih", []string{"密码："}},
		{"服务器密码是 hunter2，记得改", "hunter2", []string{"服务器密码是 ", "，记得改"}},
		{"WiFi 密碼: 12345678。", "12345678", []string{"密碼: ", "。"}},
		{"口令=Abc!2024.", "Abc!2024", []string{"口令=", "."}},
		{"パスワードは pa55word です", "pa55word", []string{"パスワードは ", " です"}},
		{"비밀번호: qwer1234", "qwer1234", []string{"비밀번호: "}},
		{`{"text":"账号 admin 密码 \"s3cr3t\""}`, "s3cr3t", []string{"账号 admin 密码 "}},
	}
	for _, c := range cases {
		out, n := Mask(c.in, Options{Secrets: true})
		if n != 1 || strings.Contains(unmarked(out), c.gone) {
			t.Errorf("%q: %d masked → %q", c.in, n, out)
		}
		for _, k := range c.kept {
			if !strings.Contains(out, k) {
				t.Errorf("%q: %q gone from %q", c.in, k, out)
			}
		}
		if back := Restore(out, false); back != c.in {
			t.Errorf("round trip: %q → %q → %q", c.in, out, back)
		}
	}
	// in a request body, as the gateway masks one
	body := `{"messages":[{"role":"user","content":"密码：786kih"}]}`
	if out, n := MaskJSON([]byte(body), Options{Secrets: true}); n != 1 || strings.Contains(string(out), "786kih") || string(RestoreJSON(out)) != body {
		t.Errorf("body: %d %s", n, out)
	}
	// what isn't a password: no value, a placeholder, words, too short
	for _, in := range []string{"请输入密码：", "密码：${DB_PASS}", "密码：****", "忘记密码了怎么办", "修改密码功能", "密码：abc"} {
		if out, n := Mask(in, Options{Secrets: true}); n != 0 {
			t.Errorf("%q masked: %q", in, out)
		}
	}
}

// A key, a secret or a token written after its name as a Chinese message
// has it is masked, by a Chinese name or an English one before 为, 為, 是 or
// ：: "密钥为 xxx" and "apikey 为 xxx" went to the vendor as written
// (CyberCaius on Discord). The name and the words around it stay.
func TestCJKSecretNamesMasked(t *testing.T) {
	cases := []struct{ in, gone, kept string }{
		{"密钥为 a8f3k2m9q1", "a8f3k2m9q1", "密钥为 "},
		{"apikey 为 AKdx93kfLq0z", "AKdx93kfLq0z", "apikey 为 "},
		{"密钥是 7f3e9c2ab41d", "7f3e9c2ab41d", "密钥是 "},
		{"密钥：Zt7qP2vX9wLm", "Zt7qP2vX9wLm", "密钥："},
		{"秘钥 q8w7e6r5t4y3", "q8w7e6r5t4y3", "秘钥 "},
		{"私钥是 MIIEvQIBADANBgkq", "MIIEvQIBADANBgkq", "私钥是 "},
		{"访问令牌：tk_8d7f6g5h4j3k", "tk_8d7f6g5h4j3k", "访问令牌："},
		{"token 是 a1b2c3d4e5f6，别外传", "a1b2c3d4e5f6", "token 是 "},
		{"我的 API Key 为 xk-2f9d8s7a6q5w。", "xk-2f9d8s7a6q5w", "API Key 为 "},
		{"API Key：xk2f9d8s7a6q5w", "xk2f9d8s7a6q5w", "API Key："},
		{"账号密码 admin123", "admin123", "账号密码 "},
		{"口令是 Zx!2025q", "Zx!2025q", "口令是 "},
		{"密码为 hunter22", "hunter22", "密码为 "},
		{"金鑰為 9Kd8s7Fq6w5e", "9Kd8s7Fq6w5e", "金鑰為 "},
		{"密鑰：Pq9w8e7r6t5y", "Pq9w8e7r6t5y", "密鑰："},
		{"access_token 是 ya29a0AfH6SMB", "ya29a0AfH6SMB", "access_token 是 "},
		{"Secret 為「s3cr3tValue9」", "s3cr3tValue9", "Secret 為「"},
		{"WiFi密码：20261025", "20261025", "WiFi密码："},
		{"admin密钥是k9j8h7g6f5", "k9j8h7g6f5", "admin密钥是"},
	}
	for _, c := range cases {
		out, n := Mask(c.in, Options{Secrets: true})
		if n != 1 || strings.Contains(unmarked(out), c.gone) || !strings.Contains(out, c.kept) {
			t.Errorf("%q: %d masked → %q", c.in, n, out)
		}
		if back := Restore(out, false); back != c.in {
			t.Errorf("round trip: %q → %q → %q", c.in, out, back)
		}
	}
	body := `{"messages":[{"role":"user","content":"apikey 为 AKdx93kfLq0z"}]}`
	if out, n := MaskJSON([]byte(body), Options{Secrets: true}); n != 1 || strings.Contains(string(out), "AKdx93kfLq0z") || string(RestoreJSON(out)) != body {
		t.Errorf("body: %d %s", n, out)
	}
	// what isn't a secret: no value, words about keys, a placeholder, a
	// number, a word, too short
	for _, in := range []string{
		"请输入密钥", "请输入密钥：", "密钥管理", "忘记密钥了怎么办", "令牌桶算法", "私钥文件放在哪",
		"密钥：${API_KEY}", "密钥：****", "token 是 ****", "apikey 为 ${OPENAI_API_KEY}",
		"RSA 密钥为 2048 位", "token 是 expired", "这个 key 是 value", "token 是 abc",
		"monkey 是 动物", "key 是什么", "API Key 为空",
	} {
		if out, n := Mask(in, Options{Secrets: true}); n != 0 {
			t.Errorf("%q masked: %q", in, out)
		}
	}
}
