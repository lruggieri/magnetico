package util

import (
	"crypto/sha1"
	"fmt"
	"math/rand"
	"time"
)

func NewRandomInfoHash() string {
	rand.Seed(time.Now().UnixNano())
	noRandomCharacters := 40 //standard infoHash length

	randString := RandomString(noRandomCharacters)

	hash := sha1.New()
	hash.Write([]byte(randString))
	bs := hash.Sum(nil)

	return fmt.Sprintf("%x", bs)
}

var characterRunes = []rune("abcdef1234567890")

// RandomString generates a random string of n length
func RandomString(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = characterRunes[rand.Intn(len(characterRunes))]
	}
	return string(b)
}
