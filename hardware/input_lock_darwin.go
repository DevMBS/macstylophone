//go:build darwin

package hardware

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework CoreFoundation -framework ApplicationServices

#include <ApplicationServices/ApplicationServices.h>
#include <CoreFoundation/CoreFoundation.h>
#include <IOKit/hidsystem/IOLLEvent.h>
#include <pthread.h>

#define KEY_BUFFER_SIZE 64

#define KEYCODE_Z 6
#define KEYCODE_X 7
#define KEYCODE_C 8
#define KEYCODE_V 9
#define KEYCODE_ESCAPE 53
#define KEYCODE_ARROW_DOWN 125
#define KEYCODE_ARROW_UP 126

static CFMachPortRef inputTap = NULL;
static CFRunLoopSourceRef inputSource = NULL;
static CFRunLoopRef inputRunLoop = NULL;
static pthread_t inputThread;
static int inputThreadStarted = 0;
static volatile int inputStopRequested = 0;
static int mouseCursorDetached = 0;

static int keyBuffer[KEY_BUFFER_SIZE];
static int keyHead = 0;
static int keyTail = 0;
static pthread_mutex_t keyMutex = PTHREAD_MUTEX_INITIALIZER;

static pthread_mutex_t initMutex = PTHREAD_MUTEX_INITIALIZER;
static pthread_cond_t initCond = PTHREAD_COND_INITIALIZER;
static int initStatus = 0;

static int isHandledKeycode(int keycode) {
	return keycode == KEYCODE_ARROW_UP ||
		keycode == KEYCODE_ARROW_DOWN ||
		keycode == KEYCODE_ESCAPE ||
		keycode == KEYCODE_Z ||
		keycode == KEYCODE_X ||
		keycode == KEYCODE_C ||
		keycode == KEYCODE_V;
}

static void pushKeyCode(int keycode) {
	pthread_mutex_lock(&keyMutex);
	int next = (keyHead + 1) % KEY_BUFFER_SIZE;
	if (next != keyTail) {
		keyBuffer[keyHead] = keycode;
		keyHead = next;
	}
	pthread_mutex_unlock(&keyMutex);
}

static int popKeyCode(void) {
	pthread_mutex_lock(&keyMutex);
	if (keyTail == keyHead) {
		pthread_mutex_unlock(&keyMutex);
		return -1;
	}

	int keycode = keyBuffer[keyTail];
	keyTail = (keyTail + 1) % KEY_BUFFER_SIZE;
	pthread_mutex_unlock(&keyMutex);

	return keycode;
}

static CGEventRef inputTapCallback(CGEventTapProxy proxy, CGEventType type, CGEventRef event, void *refcon) {
	if (type == kCGEventTapDisabledByTimeout || type == kCGEventTapDisabledByUserInput) {
		if (inputTap != NULL) {
			CGEventTapEnable(inputTap, true);
		}
		return NULL;
	}

	if (type == kCGEventKeyDown) {
		int keycode = (int)CGEventGetIntegerValueField(event, kCGKeyboardEventKeycode);
		if (isHandledKeycode(keycode)) {
			pushKeyCode(keycode);
			return NULL;
		}
		return event;
	}

	if (type == kCGEventKeyUp) {
		int keycode = (int)CGEventGetIntegerValueField(event, kCGKeyboardEventKeycode);
		if (isHandledKeycode(keycode)) {
			return NULL;
		}
		return event;
	}

	if (type == kCGEventFlagsChanged) {
		return event;
	}

	if (type == NX_SYSDEFINED ||
		type == NX_ZOOM ||
		type == 18 ||
		type == 19 ||
		type == 20 ||
		type == 29 ||
		type == 30 ||
		type == 31 ||
		type == 32 ||
		type == 34 ||
		type == 37 ||
		type == 40) {
		return NULL;
	}

	return NULL;
}

static CGEventMask inputMask(void) {
	return kCGEventMaskForAllEvents;
}

static void *inputThreadMain(void *arg) {
	CGEventMask mask = inputMask();
	CFMachPortRef tap = CGEventTapCreate(
		kCGHIDEventTap,
		kCGHeadInsertEventTap,
		kCGEventTapOptionDefault,
		mask,
		inputTapCallback,
		NULL
	);

	pthread_mutex_lock(&initMutex);
	if (tap == NULL) {
		initStatus = -1;
		pthread_cond_signal(&initCond);
		pthread_mutex_unlock(&initMutex);
		return NULL;
	}

	inputTap = tap;
	inputSource = CFMachPortCreateRunLoopSource(kCFAllocatorDefault, inputTap, 0);
	if (inputSource == NULL) {
		CFRelease(inputTap);
		inputTap = NULL;
		initStatus = -1;
		pthread_cond_signal(&initCond);
		pthread_mutex_unlock(&initMutex);
		return NULL;
	}

	inputRunLoop = CFRunLoopGetCurrent();
	CFRetain(inputRunLoop);
	CFRunLoopAddSource(inputRunLoop, inputSource, kCFRunLoopCommonModes);
	CGEventTapEnable(inputTap, true);

	initStatus = 1;
	pthread_cond_signal(&initCond);
	pthread_mutex_unlock(&initMutex);

	while (!inputStopRequested) {
		CFRunLoopRunInMode(kCFRunLoopDefaultMode, 0.2, false);
	}

	if (inputSource != NULL && inputRunLoop != NULL) {
		CFRunLoopRemoveSource(inputRunLoop, inputSource, kCFRunLoopCommonModes);
		CFRelease(inputSource);
		inputSource = NULL;
	}

	if (inputTap != NULL) {
		CFRelease(inputTap);
		inputTap = NULL;
	}

	if (inputRunLoop != NULL) {
		CFRelease(inputRunLoop);
		inputRunLoop = NULL;
	}

	return NULL;
}

static int startInputLock(void) {
	if (!AXIsProcessTrusted()) {
		return -3;
	}

	if (inputThreadStarted) {
		return 0;
	}

	CGError detachResult = CGAssociateMouseAndMouseCursorPosition(false);
	if (detachResult == kCGErrorSuccess) {
		mouseCursorDetached = 1;
	} else {
		return -4;
	}

	inputStopRequested = 0;
	pthread_mutex_lock(&keyMutex);
	keyHead = 0;
	keyTail = 0;
	pthread_mutex_unlock(&keyMutex);

	pthread_mutex_lock(&initMutex);
	initStatus = 0;
	pthread_mutex_unlock(&initMutex);

	int createResult = pthread_create(&inputThread, NULL, inputThreadMain, NULL);
	if (createResult != 0) {
		return -2;
	}

	pthread_mutex_lock(&initMutex);
	while (initStatus == 0) {
		pthread_cond_wait(&initCond, &initMutex);
	}
	int status = initStatus;
	pthread_mutex_unlock(&initMutex);

	if (status != 1) {
		pthread_join(inputThread, NULL);
		if (mouseCursorDetached) {
			CGAssociateMouseAndMouseCursorPosition(true);
			mouseCursorDetached = 0;
		}
		return -1;
	}

	inputThreadStarted = 1;
	return 0;
}

static int inputLockIsStarted(void) {
	return inputThreadStarted;
}

static void stopInputLock(void) {
	if (!inputThreadStarted) {
		if (mouseCursorDetached) {
			CGAssociateMouseAndMouseCursorPosition(true);
			mouseCursorDetached = 0;
		}
		return;
	}

	inputStopRequested = 1;
	if (inputRunLoop != NULL) {
		CFRunLoopStop(inputRunLoop);
	}

	pthread_join(inputThread, NULL);
	inputThreadStarted = 0;

	if (mouseCursorDetached) {
		CGAssociateMouseAndMouseCursorPosition(true);
		mouseCursorDetached = 0;
	}
}

static int popInputKeyCode(void) {
	return popKeyCode();
}
*/
import "C"

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

type InputKey int

const (
	InputKeyUnknown InputKey = iota
	InputKeyOctaveUp
	InputKeyOctaveDown
	InputKeyEscape
	InputKeyDrumKick
	InputKeyDrumSnare
	InputKeyDrumHiHat
	InputKeyDrumClap
)

const (
	inputKeycodeZ         = 6
	inputKeycodeX         = 7
	inputKeycodeC         = 8
	inputKeycodeV         = 9
	inputKeycodeEscape    = 53
	inputKeycodeArrowDown = 125
	inputKeycodeArrowUp   = 126
)

type InputLock struct{}

func NewInputLock() *InputLock {
	return &InputLock{}
}

func (l *InputLock) Start() error {
	result := int(C.startInputLock())
	if result == 0 {
		return nil
	}

	if result == -1 {
		exePath, _ := os.Executable()
		if strings.Contains(exePath, "/go-build/") {
			return fmt.Errorf(
				"не удалось запустить блокировку ввода: при запуске через go run используется временный бинарник без прав. Соберите постоянный бинарник и выдайте ему доступ в Accessibility/Input Monitoring (текущий exe: %s)",
				exePath,
			)
		}

		return fmt.Errorf(
			"не удалось запустить блокировку ввода: проверьте Accessibility и Input Monitoring для приложения, которое запускает процесс (текущий exe: %s)",
			exePath,
		)
	}

	if result == -3 {
		exePath, _ := os.Executable()
		return fmt.Errorf(
			"нет прав Accessibility для блокировки ввода. Проверьте, что разрешение включено для запускающего приложения и/или бинарника (текущий exe: %s)",
			exePath,
		)
	}

	if result == -4 {
		return errors.New("не удалось отключить связь трекпада/мыши с курсором (CGAssociateMouseAndMouseCursorPosition)")
	}

	return errors.New("не удалось запустить поток блокировки ввода")
}

func (l *InputLock) Stop() {
	C.stopInputLock()
}

func (l *InputLock) IsRunning() bool {
	return int(C.inputLockIsStarted()) == 1
}

func (l *InputLock) DrainKeys() []InputKey {
	keys := make([]InputKey, 0, 4)
	for {
		keycode := int(C.popInputKeyCode())
		if keycode < 0 {
			break
		}

		switch keycode {
		case inputKeycodeArrowUp:
			keys = append(keys, InputKeyOctaveUp)
		case inputKeycodeArrowDown:
			keys = append(keys, InputKeyOctaveDown)
		case inputKeycodeEscape:
			keys = append(keys, InputKeyEscape)
		case inputKeycodeZ:
			keys = append(keys, InputKeyDrumKick)
		case inputKeycodeX:
			keys = append(keys, InputKeyDrumSnare)
		case inputKeycodeC:
			keys = append(keys, InputKeyDrumHiHat)
		case inputKeycodeV:
			keys = append(keys, InputKeyDrumClap)
		default:
			keys = append(keys, InputKeyUnknown)
		}
	}

	return keys
}
