package hidden

import "testing"

func TestAddRemoveAcrossStores(t *testing.T) {
	dir := t.TempDir()
	server, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	cli, _ := Open(dir)
	if err := server.Add("claude:a"); err != nil {
		t.Fatal(err)
	}
	server.Add("claude:a") // twice is fine
	server.Add("claude:b")
	if !cli.Set()["claude:a"] || !cli.Set()["claude:b"] {
		t.Fatal("cli does not see what the server hid")
	}
	if ok, err := cli.Remove("claude:a"); !ok || err != nil {
		t.Fatalf("Remove = %v %v", ok, err)
	}
	if server.Set()["claude:a"] {
		t.Error("server still hides a session the cli showed again")
	}
	if ok, _ := cli.Remove("claude:zzz"); ok {
		t.Error("removed an id that was not hidden")
	}
	list, _ := server.List()
	if len(list) != 1 || list[0] != "claude:b" {
		t.Errorf("list = %v", list)
	}
}
