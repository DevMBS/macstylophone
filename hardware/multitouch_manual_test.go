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

// go test -tags=manual -run TestManualMultitouch -v ./hardware
func TestManualMultitouch(t *testing.T) {

	poller := NewMultitouchPoller(10 * time.Millisecond)

	frameChan := make(chan TouchFrame, 256)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		fmt.Println("\n\nОстановка...")
		cancel()
	}()

	if err := poller.StartPolling(ctx, frameChan); err != nil {
		t.Fatalf("Ошибкка при старте мультитача: %v", err)
	}

	for {
		select {
		case <-ctx.Done():
			fmt.Println("Тест завершен.")
			return
		case frame := <-frameChan:
			zone := "SYNTH"
			if frame.X >= zoneThreshold {
				zone = "DRUMS"
			}

			stateStr := "   "
			if frame.State.IsTouching() {
				stateStr = ">>>"
			}

			fmt.Printf("%s [%s] Палец:%d  X:%.4f  Y:%.4f  Сила нажатия:%.4f  Состояние:%s\n",
				stateStr, zone, frame.FingerID, frame.X, frame.Y, frame.Pressure, frame.State)
		}
	}
}

// go test -tags=manual -run TestManualMultitouchEvents -v ./hardware
func TestManualMultitouchEvents(t *testing.T) {
	fmt.Println("Зоны:")
	fmt.Println("  СЛЕВА 60%  = Синт")
	fmt.Println("  ПРАВЫЕ 40% = дрампад")
	fmt.Println()
	fmt.Println("Дрампад:")
	fmt.Println("  +--------+")
	fmt.Println("  | HiHat  |")
	fmt.Println("  +--------+")
	fmt.Println("  | Clap   |")
	fmt.Println("  +--------+")
	fmt.Println("  | Snare  |")
	fmt.Println("  +--------+")
	fmt.Println("  | Kick   |")
	fmt.Println("  +--------+")
	fmt.Println()
	fmt.Println("Нажмите Ctrl+C для выхода.")
	fmt.Println("----------------------------------------------")

	poller := NewMultitouchPoller(10 * time.Millisecond)

	eventChan := make(chan MusicEvent, 256)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		fmt.Println("\n\nОстановка...")
		cancel()
	}()

	if err := poller.StartEventPolling(ctx, eventChan); err != nil {
		t.Fatalf("Ошибка при старте мультитача: %v", err)
	}

	for {
		select {
		case <-ctx.Done():
			fmt.Println("Тест завершен")
			return
		case event := <-eventChan:
			switch event.Type {
			case EventUpdatePitch:
				pitchBar := int(event.X * 40)
				bar := ""
				for i := 0; i < pitchBar; i++ {
					bar += "="
				}
				fmt.Printf("[СИНТ] Палец:%d X: %.4f Y:%.4f  |%s>\n",
					event.FingerID, event.X, event.Y, bar)

			case EventTriggerDrum:
				fmt.Printf("[ДРАМПАД] Палец:%d  Пад:%-6s  Координата:%.2f\n",
					event.FingerID, event.DrumPad, event.Pressure)
			}
		}
	}
}
