// SPDX-License-Identifier: Apache-2.0

package auth

// FillHasher takes every password-hashing slot, as a burst of sign-ins
// would, until the returned function gives them back.
func (s *Service) FillHasher() (release func()) {
	for range maxConcurrentHashes {
		s.hasher.slots <- struct{}{}
	}
	return func() {
		for range maxConcurrentHashes {
			<-s.hasher.slots
		}
	}
}
