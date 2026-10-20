package main

import (
	"encoding/json"
	"strconv"
	"testing"
)

var sampleSnapshot = []byte(`{"t":"s","p":[[0,"c1","b1","cuang-bot",1250,50000,985.5,1000,316,10.5,50,false],[1,"c2","b2","william, jr",850,25000,1000,1000,348,12,60,false]],"f":[[101,1,12.5,-4.2,50,0,1.57],[102,2,-15.3,2.1,120,1,3.14],[103,3,5.0,1.0,250,0,0],[104,1,8.0,3.0,45,0,2.1],[105,2,-2.0,10.0,80,0,0.5]],"sh":[[501,2.5,3.1,"torpedo",1.0],[502,-1.0,0.5,"doom",5.0],[503,10.0,-2.0,"orb",1.0]],"ev":[{"k":"k","s":0,"loot":[[0,500]]}]}`)

func BenchmarkStandardJSON(b *testing.B) {
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var in struct {
			T  string          `json:"t"`
			P  [][]interface{} `json:"p"`
			F  [][]interface{} `json:"f"`
			SH [][]interface{} `json:"sh"`
		}
		_ = json.Unmarshal(sampleSnapshot, &in)
		if len(in.F) != 5 {
			b.Fatal("mismatch")
		}
	}
}

func BenchmarkFastSnapshotScanner(b *testing.B) {
	scanner := NewSnapshotScanner()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		ok := scanner.Parse(sampleSnapshot)
		if !ok || len(scanner.Fish) != 5 {
			b.Fatal("mismatch")
		}
	}
}

func TestSnapshotScannerAccuracy(t *testing.T) {
	scanner := NewSnapshotScanner()
	ok := scanner.Parse(sampleSnapshot)
	if !ok {
		t.Fatal("expected isSnapshot=true")
	}

	if len(scanner.Players) != 2 {
		t.Fatalf("expected 2 players, got %d", len(scanner.Players))
	}
	if scanner.Players[0].Slot != 0 || scanner.Players[0].Combo != 1250 || scanner.Players[0].HP != 985.5 || scanner.Players[0].IsDead {
		t.Fatalf("player 0 mismatch: %+v", scanner.Players[0])
	}
	if scanner.Players[1].Slot != 1 || scanner.Players[1].Combo != 850 || scanner.Players[1].HP != 1000 || scanner.Players[1].IsDead {
		t.Fatalf("player 1 mismatch: %+v", scanner.Players[1])
	}

	if len(scanner.Fish) != 5 {
		t.Fatalf("expected 5 fish, got %d", len(scanner.Fish))
	}
	if scanner.Fish[0].ID != 101 || scanner.Fish[0].Ty != 1 || scanner.Fish[0].X != 12.5 || scanner.Fish[0].Z != -4.2 || scanner.Fish[0].HP != 50 || scanner.Fish[0].Dir != 1.57 {
		t.Fatalf("fish 0 mismatch: %+v", scanner.Fish[0])
	}

	if len(scanner.Shots) != 3 {
		t.Fatalf("expected 3 shots, got %d", len(scanner.Shots))
	}
	if scanner.Shots[0].ID != 501 || scanner.Shots[0].Kind != "torpedo" || scanner.Shots[0].X != 2.5 || scanner.Shots[0].Z != 3.1 {
		t.Fatalf("shot 0 mismatch: %+v", scanner.Shots[0])
	}

	if len(scanner.RawEvents) != 1 {
		t.Fatalf("expected 1 raw event, got %d", len(scanner.RawEvents))
	}
}

func BenchmarkAimStandardJSON(b *testing.B) {
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		data, _ := json.Marshal(map[string]interface{}{
			"t": "aim",
			"x": 12.34,
			"z": -4.56,
			"f": true,
		})
		if len(data) == 0 {
			b.Fatal("empty")
		}
	}
}

func BenchmarkAimZeroAllocFormatting(b *testing.B) {
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var buf [64]byte
		bufSlice := buf[:0]
		bufSlice = append(bufSlice, `{"t":"aim","x":`...)
		bufSlice = strconv.AppendFloat(bufSlice, 12.34, 'f', -1, 64)
		bufSlice = append(bufSlice, `,"z":`...)
		bufSlice = strconv.AppendFloat(bufSlice, -4.56, 'f', -1, 64)
		bufSlice = append(bufSlice, `,"f":true}`...)
		if len(bufSlice) == 0 {
			b.Fatal("empty")
		}
	}
}
