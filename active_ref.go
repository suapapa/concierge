package main

import (
	"fmt"
	"os"
	"syscall"

	"github.com/goccy/go-yaml"
)

// 파일 락 획득 및 해제 헬퍼 함수
func withFileLock(fn func() error) error {
	// 락 파일 열기 (없으면 생성)
	lockFile, err := os.OpenFile(activeRefsLockFile, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return fmt.Errorf("failed to open lock file: %w", err)
	}
	defer lockFile.Close()

	// Exclusive lock 획득 (다른 프로세스가 대기)
	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("failed to acquire file lock: %w", err)
	}
	defer func() {
		if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN); err != nil {
			// Unlock 실패는 로그만 남김 (이미 함수가 종료되므로)
			fmt.Printf("warning: failed to release file lock: %v\n", err)
		}
	}()

	// 함수 실행
	return fn()
}

// activeRefs 파일 읽기 (파일 락 사용)
func readActiveRefs() (map[string]int, error) {
	var refs map[string]int
	var readErr error

	// 파일 락으로 여러 레플리카 간 동기화
	err := withFileLock(func() error {
		refs = make(map[string]int)

		data, err := os.ReadFile(activeRefsFile)
		if err != nil {
			if os.IsNotExist(err) {
				// 파일이 없으면 빈 맵 반환
				return nil
			}
			readErr = err
			return err
		}

		if len(data) == 0 {
			return nil
		}

		if err := yaml.Unmarshal(data, &refs); err != nil {
			readErr = err
			return err
		}

		return nil
	})

	if err != nil {
		return nil, err
	}
	if readErr != nil {
		return nil, readErr
	}

	if refs == nil {
		refs = make(map[string]int)
	}

	return refs, nil
}

// activeRefs에서 특정 키의 참조 수 가져오기
func getActiveRefCount(key string) (int, error) {
	refs, err := readActiveRefs()
	if err != nil {
		return 0, err
	}
	return refs[key], nil
}

// activeRefs에서 특정 키의 참조 수 증가 (원자적 연산)
func incrementActiveRef(key string) error {
	return withFileLock(func() error {
		refs := make(map[string]int)

		// 파일 읽기
		data, err := os.ReadFile(activeRefsFile)
		if err != nil && !os.IsNotExist(err) {
			return err
		}

		if len(data) > 0 {
			if err := yaml.Unmarshal(data, &refs); err != nil {
				return err
			}
		}

		// 참조 수 증가
		refs[key]++

		// 파일 쓰기
		data, err = yaml.Marshal(refs)
		if err != nil {
			return err
		}

		tmpFile := activeRefsFile + ".tmp"
		if err := os.WriteFile(tmpFile, data, 0644); err != nil {
			return err
		}

		if err := os.Rename(tmpFile, activeRefsFile); err != nil {
			os.Remove(tmpFile)
			return err
		}

		return nil
	})
}

// activeRefs에서 특정 키의 참조 수 감소 (원자적 연산)
func decrementActiveRef(key string) error {
	return withFileLock(func() error {
		refs := make(map[string]int)

		// 파일 읽기
		data, err := os.ReadFile(activeRefsFile)
		if err != nil && !os.IsNotExist(err) {
			return err
		}

		if len(data) > 0 {
			if err := yaml.Unmarshal(data, &refs); err != nil {
				return err
			}
		}

		// 참조 수 감소
		if count, exists := refs[key]; exists {
			if count <= 1 {
				delete(refs, key)
			} else {
				refs[key] = count - 1
			}
		}

		// 파일 쓰기
		data, err = yaml.Marshal(refs)
		if err != nil {
			return err
		}

		tmpFile := activeRefsFile + ".tmp"
		if err := os.WriteFile(tmpFile, data, 0644); err != nil {
			return err
		}

		if err := os.Rename(tmpFile, activeRefsFile); err != nil {
			os.Remove(tmpFile)
			return err
		}

		return nil
	})
}

// activeRefs에서 특정 키 삭제 (원자적 연산)
func deleteActiveRef(key string) error {
	return withFileLock(func() error {
		refs := make(map[string]int)

		// 파일 읽기
		data, err := os.ReadFile(activeRefsFile)
		if err != nil && !os.IsNotExist(err) {
			return err
		}

		if len(data) > 0 {
			if err := yaml.Unmarshal(data, &refs); err != nil {
				return err
			}
		}

		// 키 삭제
		delete(refs, key)

		// 파일 쓰기
		data, err = yaml.Marshal(refs)
		if err != nil {
			return err
		}

		tmpFile := activeRefsFile + ".tmp"
		if err := os.WriteFile(tmpFile, data, 0644); err != nil {
			return err
		}

		if err := os.Rename(tmpFile, activeRefsFile); err != nil {
			os.Remove(tmpFile)
			return err
		}

		return nil
	})
}
