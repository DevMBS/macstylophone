package hardware

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Foundation -framework CoreFoundation

#include <CoreFoundation/CoreFoundation.h>
#include <dlfcn.h>
#include <stdlib.h>

// типы MultitouchSupport.framework
typedef struct {
    float x;
    float y;
} MTPoint;

typedef struct {
    MTPoint position;
    MTPoint velocity;
} MTVector;

// MTTouch структура
typedef struct {
    int32_t frame;
    double timestamp;
    int32_t identifier;
    int32_t state;           // 0=не нажат, 1-4=состояние нажатия, 7=поднят палец
    int32_t fingerID;
    int32_t handID;
    MTVector normalized;     // normalized position and velocity
    float size;
    int32_t zero1;
    float angle;
    float majorAxis;
    float minorAxis;
    MTVector absolute;       // абсолютная позиция
    int32_t zero2;
    int32_t zero3;
    float density;
} MTTouch;

typedef void* MTDeviceRef;
typedef void (*MTContactCallbackFunction)(MTDeviceRef device, MTTouch *touches, int numTouches, double timestamp, int frame);

static void* mtlib = NULL;
static MTDeviceRef (*pMTDeviceCreateDefault)(void) = NULL;
static void (*pMTRegisterContactFrameCallback)(MTDeviceRef, MTContactCallbackFunction) = NULL;
static void (*pMTDeviceStart)(MTDeviceRef, int) = NULL;
static void (*pMTDeviceStop)(MTDeviceRef) = NULL;
static CFArrayRef (*pMTDeviceCreateList)(void) = NULL;

#define MAX_TOUCHES 16

typedef struct {
    int fingerID;
    float x;
    float y;
    float pressure;
    int state;
    double timestamp;
} TouchData;

static TouchData touchBuffer[MAX_TOUCHES];
static int touchCount = 0;
static int newDataAvailable = 0;

// Колбэк вызываемый MultitouchSupport
static void touchCallback(MTDeviceRef device, MTTouch *touches, int numTouches, double timestamp, int frame) {
    if (numTouches > MAX_TOUCHES) {
        numTouches = MAX_TOUCHES;
    }

    for (int i = 0; i < numTouches; i++) {
        touchBuffer[i].fingerID = touches[i].identifier;
        touchBuffer[i].x = touches[i].normalized.position.x;
        touchBuffer[i].y = touches[i].normalized.position.y;
        touchBuffer[i].pressure = touches[i].size;
        touchBuffer[i].state = touches[i].state;
        touchBuffer[i].timestamp = timestamp;
    }
    touchCount = numTouches;
    newDataAvailable = 1;
}

// Инициализация фремворка MultitouchSupport
static int initMultitouch(void) {
    if (mtlib != NULL) {
        return 0;
    }

    mtlib = dlopen("/System/Library/PrivateFrameworks/MultitouchSupport.framework/MultitouchSupport", RTLD_LAZY);
    if (!mtlib) {
        return -1;
    }

    pMTDeviceCreateDefault = dlsym(mtlib, "MTDeviceCreateDefault");
    pMTRegisterContactFrameCallback = dlsym(mtlib, "MTRegisterContactFrameCallback");
    pMTDeviceStart = dlsym(mtlib, "MTDeviceStart");
    pMTDeviceStop = dlsym(mtlib, "MTDeviceStop");
    pMTDeviceCreateList = dlsym(mtlib, "MTDeviceCreateList");

    if (!pMTDeviceCreateDefault || !pMTRegisterContactFrameCallback ||
        !pMTDeviceStart || !pMTDeviceStop) {
        dlclose(mtlib);
        mtlib = NULL;
        return -2;
    }

    return 0;
}

static MTDeviceRef currentDevice = NULL;

// Старт захвата тачпада
static int startMultitouch(void) {
    if (initMultitouch() != 0) {
        return -1;
    }

    if (pMTDeviceCreateList) {
        CFArrayRef devices = pMTDeviceCreateList();
        if (devices && CFArrayGetCount(devices) > 0) {
            currentDevice = (MTDeviceRef)CFArrayGetValueAtIndex(devices, 0);
        }
    }

    if (!currentDevice) {
        currentDevice = pMTDeviceCreateDefault();
    }

    if (!currentDevice) {
        return -2;
    }

    pMTRegisterContactFrameCallback(currentDevice, touchCallback);
    pMTDeviceStart(currentDevice, 0);

    return 0;
}

// Остановка захвата
static void stopMultitouch(void) {
    if (currentDevice && pMTDeviceStop) {
        pMTDeviceStop(currentDevice);
        currentDevice = NULL;
    }
}

// Проверка новых данных
static int hasNewData(void) {
    return newDataAvailable;
}

static int getTouchCount(void) {
    return touchCount;
}

static TouchData getTouchAt(int index) {
    if (index >= 0 && index < touchCount) {
        return touchBuffer[index];
    }
    TouchData empty = {0};
    return empty;
}

static void clearNewDataFlag(void) {
    newDataAvailable = 0;
}
*/
import "C"

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"
)

// TouchState определяет состояние пальца (не касается, касается, поднят)
type TouchState int

const (
	TouchStateNotTouching TouchState = 0
	TouchStateTouching    TouchState = 1
	TouchStateLifted      TouchState = 7
)

func (s TouchState) String() string {
	switch s {
	case TouchStateNotTouching:
		return "NotTouching"
	case TouchStateLifted:
		return "Lifted"
	default:
		if s >= 1 && s <= 6 {
			return "Touching"
		}
		return "Unknown"
	}
}

// IsTouching возвращает true если зарегистрировано касание
func (s TouchState) IsTouching() bool {
	return s >= 1 && s <= 6
}

// TouchFrame определяет точку касания
type TouchFrame struct {
	FingerID  int        `json:"finger_id"`
	X         float64    `json:"x"`
	Y         float64    `json:"y"`
	Pressure  float64    `json:"pressure"`
	State     TouchState `json:"state"`
	Timestamp float64    `json:"timestamp"`
}

// EventType тип события синта
type EventType int

const (
	EventUpdatePitch EventType = iota
	EventTriggerDrum
)

func (e EventType) String() string {
	switch e {
	case EventUpdatePitch:
		return "UpdatePitch"
	case EventTriggerDrum:
		return "TriggerDrum"
	default:
		return "Unknown"
	}
}

// MusicEvent определяет состояние синта из данных касания
type MusicEvent struct {
	Type      EventType `json:"type"`
	FingerID  int       `json:"finger_id"`
	X         float64   `json:"x"`
	Y         float64   `json:"y"`
	Pressure  float64   `json:"pressure"`
	DrumPad   string    `json:"drum_pad,omitempty"`
	Timestamp float64   `json:"timestamp"`
}

// Разделение зоны, 60% на синт, 40 на дрампад
const zoneThreshold = 0.6

// Идентификаторы дрампада
const (
	PadKick  = "kick"
	PadSnare = "snare"
	PadHiHat = "hihat"
	PadClap  = "clap"
)

// MultitouchPoller обрабатывает сырые данные с тачпада
type MultitouchPoller struct {
	mu           sync.Mutex
	running      bool
	pollInterval time.Duration
	fingerStates map[int]bool
}

// NewMultitouchPoller Создает новый поллер
func NewMultitouchPoller(pollInterval time.Duration) *MultitouchPoller {
	return &MultitouchPoller{
		pollInterval: pollInterval,
		fingerStates: make(map[int]bool),
	}
}

// Start инициализирует фреймворк мультитача и начинает захват
func (p *MultitouchPoller) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.running {
		return errors.New("мультитач поллер уже запущен")
	}

	result := C.startMultitouch()
	if result != 0 {
		switch result {
		case -1:
			return errors.New("ошибка загрузки MultitouchSupport.framework")
		case -2:
			return errors.New("ошибка загрузки multitouch device")
		default:
			return errors.New("неизвестная ошибка при старте мультитача")
		}
	}

	p.running = true
	log.Println("[multitouch] Начался захват тачпада")
	return nil
}

// Stop останавливает захват
func (p *MultitouchPoller) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.running {
		return
	}

	C.stopMultitouch()
	p.running = false
	log.Println("[multitouch] Остановлен захват тачпада")
}

// StartPolling Начинает поллинг мультитача и отправляет в канал
func (p *MultitouchPoller) StartPolling(ctx context.Context, frameChan chan<- TouchFrame) error {
	if err := p.Start(); err != nil {
		return err
	}

	go p.pollLoop(ctx, frameChan)
	return nil
}

// StartEventPolling Начинает поллинг и отправляет действия синта
func (p *MultitouchPoller) StartEventPolling(ctx context.Context, eventChan chan<- MusicEvent) error {
	if err := p.Start(); err != nil {
		return err
	}

	go p.eventLoop(ctx, eventChan)
	return nil
}

// pollLoop поллинг данных нажатий
func (p *MultitouchPoller) pollLoop(ctx context.Context, frameChan chan<- TouchFrame) {
	ticker := time.NewTicker(p.pollInterval)
	defer ticker.Stop()
	defer p.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("[multitouch] Поллинг остановлен")
			return
		case <-ticker.C:
			frames := p.readFrames()
			for _, frame := range frames {
				select {
				case frameChan <- frame:
				default:
					// Канал полон
				}
			}
		}
	}
}

// eventLoop генерирует события синта
func (p *MultitouchPoller) eventLoop(ctx context.Context, eventChan chan<- MusicEvent) {
	ticker := time.NewTicker(p.pollInterval)
	defer ticker.Stop()
	defer p.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("[multitouch] Цикл остановлен")
			return
		case <-ticker.C:
			events := p.processEvents()
			for _, event := range events {
				select {
				case eventChan <- event:
				default:
					// Канал полон
				}
			}
		}
	}
}

// readFrames читает фреймы из C буфера
func (p *MultitouchPoller) readFrames() []TouchFrame {
	if C.hasNewData() == 0 {
		return nil
	}

	count := int(C.getTouchCount())
	frames := make([]TouchFrame, 0, count)

	for i := 0; i < count; i++ {
		data := C.getTouchAt(C.int(i))
		frame := TouchFrame{
			FingerID:  int(data.fingerID),
			X:         float64(data.x),
			Y:         float64(data.y),
			Pressure:  float64(data.pressure),
			State:     TouchState(data.state),
			Timestamp: float64(data.timestamp),
		}
		frames = append(frames, frame)
	}

	C.clearNewDataFlag()
	return frames
}

// processEvents читает фреймы и генерирует события синта
func (p *MultitouchPoller) processEvents() []MusicEvent {
	frames := p.readFrames()
	if len(frames) == 0 {
		return nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	var events []MusicEvent

	for _, frame := range frames {
		wasTouching := p.fingerStates[frame.FingerID]
		isTouching := frame.State.IsTouching()

		p.fingerStates[frame.FingerID] = isTouching

		if frame.X >= zoneThreshold {
			// Зона дрампада
			// Триггер только при касании
			if isTouching && !wasTouching {
				event := MusicEvent{
					Type:      EventTriggerDrum,
					FingerID:  frame.FingerID,
					X:         (frame.X - zoneThreshold) / (1.0 - zoneThreshold),
					Y:         frame.Y,
					Pressure:  frame.Pressure,
					DrumPad:   p.determineDrumPad(frame.X, frame.Y),
					Timestamp: frame.Timestamp,
				}
				events = append(events, event)
			}
		} else {
			// Зона синта
			// Бесконечное обновление координаты
			if isTouching {
				event := MusicEvent{
					Type:      EventUpdatePitch,
					FingerID:  frame.FingerID,
					X:         frame.X / zoneThreshold,
					Y:         frame.Y,
					Pressure:  frame.Pressure,
					Timestamp: frame.Timestamp,
				}
				events = append(events, event)
			}
		}
	}

	// Очистка состояний
	for id := range p.fingerStates {
		found := false
		for _, frame := range frames {
			if frame.FingerID == id {
				found = true
				break
			}
		}
		if !found {
			delete(p.fingerStates, id)
		}
	}

	return events
}

// determineDrumPad Определяет какой инструмент в какой зоне
//
//	+--------+
//	| HiHat  |  Y >= 0.75
//	+--------+
//	| Clap   |  Y >= 0.50
//	+--------+
//	| Snare  |  Y >= 0.25
//	+--------+
//	| Kick   |  Y < 0.25
//	+--------+
func (p *MultitouchPoller) determineDrumPad(x, y float64) string {
	switch {
	case y >= 0.75:
		return PadHiHat
	case y >= 0.50:
		return PadClap
	case y >= 0.25:
		return PadSnare
	default:
		return PadKick
	}
}
