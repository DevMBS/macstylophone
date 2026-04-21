//go:build manual && darwin

package hardware

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"
)

// go test -tags=manual -run TestGranularMultitouch -v ./hardware
func TestGranularMultitouch(t *testing.T) {
	fmt.Println("=== СТИЛОФОН (1 ОКТАВА + ЭФФЕКТЫ) ===")
	fmt.Println("Ось X: Ноты (C - B)")
	fmt.Println("Ось Y: Уровень эффекта (0% - 100%)")
	fmt.Println("Нажмите Ctrl+C для выхода.")
	fmt.Println("----------------------------------------------")

	poller := NewMultitouchPoller(15 * time.Millisecond)
	mapper := NewTouchpadMapper(4, 0, 8)
	inputLock := NewInputLock()
	gestureSuppressor := NewDockOnlyGestureSuppressor()
	eventChan := make(chan StylophoneEvent, 256)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		fmt.Println("\n\nОстановка...")
		cancel()
	}()

	if err := poller.StartStylophonePolling(ctx, mapper, eventChan); err != nil {
		t.Fatalf("Ошибка при старте мультитача: %v", err)
	}

	if err := gestureSuppressor.Start(); err != nil {
		t.Fatalf("Ошибка при отключении системных жестов: %v", err)
	}
	defer gestureSuppressor.Stop()

	if err := inputLock.Start(); err != nil {
		t.Fatalf("Ошибка при блокировке курсора: %v", err)
	}
	defer inputLock.Stop()
	if !inputLock.IsRunning() {
		t.Fatal("блокировка ввода не запущена")
	}

	fmt.Printf("Текущая октава: %d\n", mapper.CurrentOctave())
	fmt.Println("Курсор заблокирован, системные жесты временно отключены. Esc для выхода, стрелки вверх/вниз меняют октаву.")

	keyTicker := time.NewTicker(10 * time.Millisecond)
	defer keyTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			fmt.Println("Тест завершен")
			return
		case <-keyTicker.C:
			for _, key := range inputLock.DrainKeys() {
				switch key {
				case InputKeyOctaveUp:
					fmt.Printf("[\033[33mOCTAVE\033[0m] Октава повышена: %d\n", mapper.ShiftOctave(1))
				case InputKeyOctaveDown:
					fmt.Printf("[\033[33mOCTAVE\033[0m] Октава понижена: %d\n", mapper.ShiftOctave(-1))
				case InputKeyDrumKick:
					fmt.Println("[DRUM] kick (z)")
				case InputKeyDrumSnare:
					fmt.Println("[DRUM] snare (x)")
				case InputKeyDrumHiHat:
					fmt.Println("[DRUM] hihat (c)")
				case InputKeyDrumClap:
					fmt.Println("[DRUM] clap (v)")
				case InputKeyEscape:
					fmt.Println("Получен Esc, завершение теста...")
					cancel()
				}
			}
		case event := <-eventChan:
			switch event.Type {
			case EventSynthStart.String():
				fmt.Printf("\033[32m[START]\033[0m Палец %d | Нота: \033[1m%-4s\033[0m | Эффект: %3d%%\n", event.FingerID, event.Note, event.EffectLevel)
			case EventSynthMove.String():
				fmt.Printf(" [MOVE] Палец %d | Нота: \033[1m%-4s\033[0m | Эффект: %3d%%\n", event.FingerID, event.Note, event.EffectLevel)
			case EventSynthEnd.String():
				fmt.Printf("\033[31m[ END ]\033[0m Палец %d | Отпущен\n", event.FingerID)
			}
		}
	}
}
