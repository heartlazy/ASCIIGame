package protocol

import "testing"

func BenchmarkParse(b *testing.B) {
	raw := "GAME_STATE|1234567890|1,10,20,100,15,5,5,0,0,0,0,0,0;2,30,15,80,25,5,5,1,1,2,0,0,0|5,5,1;6,6,2;10,10,3|25\n"
	for i := 0; i < b.N; i++ {
		_, _ = Parse(raw)
	}
}

func BenchmarkBuildGameState(b *testing.B) {
	players := "1,10,20,100,15,5,5,0,0,0,0,0,0;2,30,15,80,25,5,5,1,1,2,0,0,0"
	items := "5,5,1;6,6,2;10,10,3"
	for i := 0; i < b.N; i++ {
		_ = BuildGameState(1234567890, players, items, 25)
	}
}
