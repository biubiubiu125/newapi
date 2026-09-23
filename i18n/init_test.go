package i18n

import (
	"testing"
	"testing/fstest"

	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
)

func TestInitFromFSRetriesAfterFailure(t *testing.T) {
	initMu.Lock()
	origInitialized := initialized
	origBundle := bundle
	origLocalizers := localizers
	initialized = false
	bundle = nil
	localizers = make(map[string]*goi18n.Localizer)
	initMu.Unlock()

	t.Cleanup(func() {
		initMu.Lock()
		initialized = origInitialized
		bundle = origBundle
		localizers = origLocalizers
		initMu.Unlock()
	})

	if err := initFromFS(fstest.MapFS{}); err == nil {
		t.Fatal("expected first load from empty FS to fail")
	}
	initMu.Lock()
	retryable := !initialized
	initMu.Unlock()
	if !retryable {
		t.Fatal("failed init must remain retryable")
	}

	if err := initFromFS(localeFS); err != nil {
		t.Fatal(err)
	}
	initMu.Lock()
	ok := initialized && bundle != nil && len(localizers) > 0
	initMu.Unlock()
	if !ok {
		t.Fatal("successful init should mark initialized and keep locales")
	}
}

func TestTranslateDoesNotPanicWhenUninitialized(t *testing.T) {
	initMu.Lock()
	origInitialized := initialized
	origBundle := bundle
	origLocalizers := localizers
	initialized = false
	bundle = nil
	localizers = nil
	initMu.Unlock()

	t.Cleanup(func() {
		initMu.Lock()
		initialized = origInitialized
		bundle = origBundle
		localizers = origLocalizers
		initMu.Unlock()
	})

	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("uninitialized catalog panicked: %v", recovered)
		}
	}()

	if got := Translate(LangEn, MsgInvalidParams); got != MsgInvalidParams {
		t.Fatalf("Translate()=%q want key fallback %q", got, MsgInvalidParams)
	}
	if loc := GetLocalizer(LangZhCN); loc == nil {
		t.Fatal("GetLocalizer() returned nil")
	}
	if got := ProtocolMessage(MsgInvalidParams); got == "" {
		t.Fatal("ProtocolMessage() returned empty string")
	}
}
