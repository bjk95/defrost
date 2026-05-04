package resultcollector

import (
	"math/rand"
	"testing"
)

func TestSeventyPercentPass(t *testing.T){
	random := rand.Float32()
	if random < 2 {
		t.Error("result not 0")
	}
}