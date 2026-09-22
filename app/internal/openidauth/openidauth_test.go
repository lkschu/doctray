package openidauth

import (
	"testing"
	"time"
)

func TestPendingAuthTransactionsKeepLoginFlowsIndependent(t *testing.T) {
	transactions := newPendingAuthTransactions()
	expiresAt := time.Now().Add(time.Minute)

	if !transactions.add("first-state", pendingAuthTransaction{
		nonce:     "first-nonce",
		expiresAt: expiresAt,
	}) {
		t.Fatal("first pending login was not stored")
	}
	if !transactions.add("second-state", pendingAuthTransaction{
		nonce:     "second-nonce",
		expiresAt: expiresAt,
	}) {
		t.Fatal("second pending login was not stored")
	}

	first, ok := transactions.take("first-state")
	if !ok || first.nonce != "first-nonce" {
		t.Fatal("first pending login was not retained")
	}

	second, ok := transactions.take("second-state")
	if !ok || second.nonce != "second-nonce" {
		t.Fatal("second pending login was not retained")
	}

	if _, ok := transactions.take("first-state"); ok {
		t.Fatal("a pending login must be consumed only once")
	}
}
