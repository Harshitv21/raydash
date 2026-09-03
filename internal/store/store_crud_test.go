package store

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
);

func TestStoreInit(t *testing.T) {
	store := NewStore();

	if(store == nil) {
		t.Fatal("Expected a pointer of NewStore but got nil");
	}
	if(store.data == nil) {
		t.Error("Internal map is not initialized got nil");
	}
}

func TestStoreSetAndGet(t *testing.T) {
	store := NewStore();

	store.Set("example_uuid_1", "Harshit");

	// these are basically test cases for our store?
	tests := []struct {
		name 		 string
		key 		 string
		wantedVal    string
		wantedExists bool
	}{
		{
			name: 		  "Existing key",
			key:  		  "example_uuid_1",
			wantedVal:    "Harshit",
			wantedExists: true,
		},
		{
			name: 		  "Non existent key",
			key:  		  "example_uuid_2",
			wantedVal:    "",
			wantedExists: false,
		},
	}

	for _, tCases := range tests {
		t.Run(tCases.name, func(t *testing.T) {
			val, exists := store.Get(tCases.key);

			if val != tCases.wantedVal {
				t.Errorf("Get() val: %v, wanted: %v", val, tCases.wantedVal);
			}
			if exists != tCases.wantedExists {
				t.Errorf("Get() exists: %v, wanted: %v", exists, tCases.wantedExists);
			}
		})
	}
}

func TestStoreDelete(t *testing.T) {
	store := NewStore();

	store.Set("example_uuid_3", "Hi");

	_, exists := store.Get("example_uuid_3");
	if(!exists) {
		t.Fatal("Key should exist before deletion!");
	}

	store.Delete("example_uuid_3");

	_, exists = store.Get("example_uuid_3");
	if(exists) {
		t.Error("Expected this key to be gone but it is still present");
	}

	store.Delete("example_uuid_non_existent");
}

func TestStoreIterate(t *testing.T) {
	store := NewStore();

	store.Set("example_uuid_1", "Hi");
	store.Set("example_uuid_2", "Harshit");
	store.Set("example_uuid_3", "How");
	store.Set("example_uuid_4", "Are");
	store.Set("example_uuid_5", "You?");

	oldStdout := os.Stdout;

	r, w, _ := os.Pipe();
	os.Stdout = w;

	store.Iterate();

	w.Close();
	os.Stdout = oldStdout;
	var buff bytes.Buffer;
	io.Copy(&buff, r);
	output := buff.String();

	expectedOutputs := []string{
		"Key: example_uuid_1; Value: Hi",
		"Key: example_uuid_2; Value: Harshit",
		"Key: example_uuid_3; Value: How",
		"Key: example_uuid_4; Value: Are",
		"Key: example_uuid_5; Value: You?",
	}

	for _, expected := range expectedOutputs {
		if !strings.Contains(output, expected) {
			t.Errorf("Iterate() output is missing! expected: %q, got: %q", expected, output);
		}
	}
}