// SPDX-License-Identifier: Apache-2.0
package main

import "testing"

func TestAckBodyRejectsNonInteger(t *testing.T) {
	for _, bad := range []string{"", "-1", "1,\"x\":2", "1}", "abc", "18446744073709551616"} {
		if _, err := ackBody(bad); err == nil {
			t.Errorf("ackBody(%q) accepted", bad)
		}
	}
	b, err := ackBody("007")
	if err != nil || string(b) != `{"sequence":7}` {
		t.Fatalf("got %s %v", b, err)
	}
}
