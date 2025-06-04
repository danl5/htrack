package state

import (
	"fmt"
	"sync"
	"time"
)

// StateMachine 状态机接口
type StateMachine interface {
	GetCurrentState() interface{}
	Transition(event *StateMachineEvent) error
	AddEventHandler(eventType string, handler EventHandler) error
	RemoveEventHandler(eventType string) error
	GetHistory() []*StateTransition
	Reset() error
}

// EventHandler 事件处理器
type EventHandler func(sm StateMachine, event *StateMachineEvent) (interface{}, error)

// GenericStateMachine 通用状态机实现
type GenericStateMachine struct {
	mu            sync.RWMutex
	id            string
	currentState  interface{}
	initialState  interface{}
	validator     StateValidator
	eventHandlers map[string]EventHandler
	history       []*StateTransition
	maxHistory    int
	createdAt     time.Time
	lastUpdate    time.Time
}

// NewGenericStateMachine 创建新的通用状态机
func NewGenericStateMachine(id string, initialState interface{}, validator StateValidator) *GenericStateMachine {
	return &GenericStateMachine{
		id:            id,
		currentState:  initialState,
		initialState:  initialState,
		validator:     validator,
		eventHandlers: make(map[string]EventHandler),
		history:       make([]*StateTransition, 0),
		maxHistory:    100, // 默认保留100条历史记录
		createdAt:     time.Now(),
		lastUpdate:    time.Now(),
	}
}

// GetCurrentState 获取当前状态
func (sm *GenericStateMachine) GetCurrentState() interface{} {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.currentState
}

// Transition 状态转换
func (sm *GenericStateMachine) Transition(event *StateMachineEvent) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// 查找事件处理器
	handler, exists := sm.eventHandlers[event.Type]
	if !exists {
		return fmt.Errorf("%w: %s", ErrEventHandlerNotFound, event.Type)
	}

	// 执行事件处理器获取新状态
	newState, err := handler(sm, event)
	if err != nil {
		return err
	}

	// 如果新状态为nil，表示不需要状态转换
	if newState == nil {
		return nil
	}

	// 验证状态转换
	if sm.validator != nil {
		if err := sm.validator.ValidateTransition(sm.currentState, newState, event.Type); err != nil {
			return err
		}
	}

	// 记录状态转换
	transition := &StateTransition{
		From:      sm.currentState,
		To:        newState,
		Event:     event.Type,
		Timestamp: time.Now(),
		Data:      event.Data,
	}

	// 更新状态
	sm.currentState = newState
	sm.lastUpdate = time.Now()

	// 添加到历史记录
	sm.addToHistory(transition)

	return nil
}

// AddEventHandler 添加事件处理器
func (sm *GenericStateMachine) AddEventHandler(eventType string, handler EventHandler) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.eventHandlers[eventType] = handler
	return nil
}

// RemoveEventHandler 移除事件处理器
func (sm *GenericStateMachine) RemoveEventHandler(eventType string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	delete(sm.eventHandlers, eventType)
	return nil
}

// GetHistory 获取状态转换历史
func (sm *GenericStateMachine) GetHistory() []*StateTransition {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	// 返回历史记录的副本
	history := make([]*StateTransition, len(sm.history))
	copy(history, sm.history)
	return history
}

// Reset 重置状态机
func (sm *GenericStateMachine) Reset() error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.currentState = sm.initialState
	sm.history = sm.history[:0]
	sm.lastUpdate = time.Now()
	return nil
}

// addToHistory 添加到历史记录
func (sm *GenericStateMachine) addToHistory(transition *StateTransition) {
	sm.history = append(sm.history, transition)

	// 如果超过最大历史记录数，移除最旧的记录
	if len(sm.history) > sm.maxHistory {
		sm.history = sm.history[1:]
	}
}

// GetID 获取状态机ID
func (sm *GenericStateMachine) GetID() string {
	return sm.id
}

// GetCreatedAt 获取创建时间
func (sm *GenericStateMachine) GetCreatedAt() time.Time {
	return sm.createdAt
}

// GetLastUpdate 获取最后更新时间
func (sm *GenericStateMachine) GetLastUpdate() time.Time {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.lastUpdate
}

// SetMaxHistory 设置最大历史记录数
func (sm *GenericStateMachine) SetMaxHistory(max int) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.maxHistory = max

	// 如果当前历史记录超过新的最大值，截断
	if len(sm.history) > max {
		sm.history = sm.history[len(sm.history)-max:]
	}
}

// StateMachineManager 状态机管理器
type StateMachineManager struct {
	mu            sync.RWMutex
	stateMachines map[string]StateMachine
}

// NewStateMachineManager 创建新的状态机管理器
func NewStateMachineManager() *StateMachineManager {
	return &StateMachineManager{
		stateMachines: make(map[string]StateMachine),
	}
}

// AddStateMachine 添加状态机
func (smm *StateMachineManager) AddStateMachine(id string, sm StateMachine) error {
	smm.mu.Lock()
	defer smm.mu.Unlock()

	if _, exists := smm.stateMachines[id]; exists {
		return fmt.Errorf("state machine with id %s already exists", id)
	}

	smm.stateMachines[id] = sm
	return nil
}

// GetStateMachine 获取状态机
func (smm *StateMachineManager) GetStateMachine(id string) (StateMachine, error) {
	smm.mu.RLock()
	defer smm.mu.RUnlock()

	sm, exists := smm.stateMachines[id]
	if !exists {
		return nil, fmt.Errorf("%w: %s", ErrStateMachineNotFound, id)
	}

	return sm, nil
}

// RemoveStateMachine 移除状态机
func (smm *StateMachineManager) RemoveStateMachine(id string) error {
	smm.mu.Lock()
	defer smm.mu.Unlock()

	if _, exists := smm.stateMachines[id]; !exists {
		return fmt.Errorf("%w: %s", ErrStateMachineNotFound, id)
	}

	delete(smm.stateMachines, id)
	return nil
}

// ListStateMachines 列出所有状态机ID
func (smm *StateMachineManager) ListStateMachines() []string {
	smm.mu.RLock()
	defer smm.mu.RUnlock()

	ids := make([]string, 0, len(smm.stateMachines))
	for id := range smm.stateMachines {
		ids = append(ids, id)
	}
	return ids
}

// ProcessEvent 处理事件
func (smm *StateMachineManager) ProcessEvent(id string, event *StateMachineEvent) error {
	sm, err := smm.GetStateMachine(id)
	if err != nil {
		return err
	}

	return sm.Transition(event)
}

// GetAllStates 获取所有状态机的当前状态
func (smm *StateMachineManager) GetAllStates() map[string]interface{} {
	smm.mu.RLock()
	defer smm.mu.RUnlock()

	states := make(map[string]interface{})
	for id, sm := range smm.stateMachines {
		states[id] = sm.GetCurrentState()
	}
	return states
}

// ResetAll 重置所有状态机
func (smm *StateMachineManager) ResetAll() error {
	smm.mu.RLock()
	defer smm.mu.RUnlock()

	for _, sm := range smm.stateMachines {
		if err := sm.Reset(); err != nil {
			return err
		}
	}
	return nil
}

// Count 获取状态机数量
func (smm *StateMachineManager) Count() int {
	smm.mu.RLock()
	defer smm.mu.RUnlock()
	return len(smm.stateMachines)
}
