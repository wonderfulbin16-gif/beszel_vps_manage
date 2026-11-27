package system

import (
	"encoding/json"
	"testing"

	"github.com/fxamacker/cbor/v2"
)

func TestSandboxDiskWireSnapshot(t *testing.T) {
	zero := 0.0
	input := CombinedData{
		Info:  Info{SandboxDiskEnabled: true, SandboxDiskUsed: &zero},
		Stats: Stats{SandboxDiskUsed: &zero},
	}
	encoded, err := cbor.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var output CombinedData
	if err := cbor.Unmarshal(encoded, &output); err != nil {
		t.Fatal(err)
	}
	if !output.Info.SandboxDiskEnabled || output.Info.SandboxDiskUsed == nil || *output.Info.SandboxDiskUsed != 0 ||
		output.Stats.SandboxDiskUsed == nil || *output.Stats.SandboxDiskUsed != 0 {
		t.Fatalf("zero usage or mode lost in CBOR: %#v", output)
	}

	for _, tc := range []struct {
		info Info
		mode bool
		used bool
	}{
		{info: Info{}},
		{info: Info{SandboxDiskEnabled: true}, mode: true},
		{info: output.Info, mode: true, used: true},
	} {
		data, err := json.Marshal(tc.info)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(data, &fields); err != nil {
			t.Fatal(err)
		}
		if (fields["sde"] != nil) != tc.mode || (fields["sdu"] != nil) != tc.used {
			t.Fatalf("unexpected field presence in %s", data)
		}
	}
}
