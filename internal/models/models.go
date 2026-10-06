package models

import "time"

// 服装状态：同一件实物服装在其生命周期内的状态。
const (
	CostumeAvailable = "available" // 可正常分配
	CostumeInUse     = "in_use"    // 演出中已被演员签收穿着
	CostumeReturned  = "returned"  // 散场交回，待清洁/检查
	CostumeWashing   = "washing"   // 洗护中
	CostumeRepairing = "repairing" // 维修中
	CostumeLost      = "lost"      // 少件（待处理事件未关闭）
	CostumeDamaged   = "damaged"   // 破损（待处理事件未关闭）
	CostumeRetired   = "retired"   // 报废
)

// 交接任务状态。
const (
	TaskPending   = "pending"   // 尚未执行
	TaskSigned    = "signed"    // 演员已签收穿上
	TaskCollected = "collected" // 已从演员处收回（交回换装间/洗衣房）
	TaskSkipped   = "skipped"   // 临时换角后不再执行
)

// 事件类型与处理状态。
const (
	IncidentLost    = "lost"
	IncidentDamaged = "damaged"

	IncidentOpen     = "open"
	IncidentResolved = "resolved" // 找回/修复并洗护完成
	IncidentWritten  = "written_off"
)

// Actor 演员/角色。换装任务按“演员上场顺序”执行，
// 一个演员在同一时间只能处理一件身上的衣服。
type Actor struct {
	ID        uint   `json:"id" gorm:"primaryKey"`
	Name      string `json:"name" gorm:"uniqueIndex;not null"`
	Role      string `json:"role"`
	Phone     string `json:"phone"`
	CreatedAt time.Time `json:"created_at"`
}

// Costume 服装实物。每件衣服独立编号、独立追溯，
// 不设“库存数量”来掩盖少件/破损。
type Costume struct {
	ID            uint      `json:"id" gorm:"primaryKey"`
	Code          string    `json:"code" gorm:"uniqueIndex;not null"` // 实物编号/条码
	Name          string    `json:"name"`                            // 服装名称（如：黑色燕尾服 L 码）
	Category      string    `json:"category"`
	Status        string    `json:"status" gorm:"index;not null;default:'available'"`
	ChangeSeconds int       `json:"change_seconds" gorm:"not null;default:120"` // 预计换装耗时（秒）
	CleanSeconds  int       `json:"clean_seconds" gorm:"not null;default:1800"`  // 最短清洁间隔（秒）
	// 最近一次洗护完成时间：再分配时校验 clean_seconds 间隔的依据。
	LastCleanedAt *time.Time `json:"last_cleaned_at"`
	// 当前所在位置（换装间/洗衣房/维修间/某演员）。
	CurrentLocation string    `json:"current_location" gorm:"default:'主换装间'"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// Performance 一场演出。
type Performance struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	Name      string    `json:"name" gorm:"not null"`
	Venue     string    `json:"venue"`
	ShowTime  time.Time `json:"show_time" gorm:"not null"` // 演出开场时间
	Status    string    `json:"status" gorm:"default:'scheduled'"` // scheduled / closed
	CreatedAt time.Time `json:"created_at"`
}

// Assignment 一次换装交接任务：某场演出中，某件衣服在某个换装点
// 交给某位演员，演员用完后在下一个交接点交回/转给下一位。
// 按 Sequence（上场顺序）排列即构成换装路线。
type Assignment struct {
	ID           uint      `json:"id" gorm:"primaryKey"`
	PerformanceID uint     `json:"performance_id" gorm:"index;not null"`
	CostumeID    uint      `json:"costume_id" gorm:"index;not null"`
	ActorID      uint      `json:"actor_id" gorm:"index;not null"`
	// PlannedActorID 记录换角前的原定演员；已签收任务换角后仍关联原演员，
	// 通过该字段保留换角痕迹。未换角时与 ActorID 相同。
	PlannedActorID uint   `json:"planned_actor_id" gorm:"not null"`
	// Sequence 该场演出中的上场/换装顺序（从 1 开始）。
	Sequence int `json:"sequence" gorm:"not null"`
	Scene    string `json:"scene"` // 场次/场景，如“第二幕”
	// HandoverAt 计划交接（签收）时间。
	HandoverAt time.Time `json:"handover_at" gorm:"not null"`
	// NeedByAt 演员必须穿好上场的时间（ deadline ）。
	NeedByAt time.Time `json:"need_by_at" gorm:"not null"`
	// ReturnAt 计划交回时间（衣服离开演员的时间）。
	ReturnAt time.Time `json:"return_at" gorm:"not null"`
	// ChangePoint 换装点，如“上场门侧台”。
	ChangePoint string `json:"change_point"`
	Status      string `json:"status" gorm:"index;not null;default:'pending'"`

	// 执行记录
	SignedAt    *time.Time `json:"signed_at"`
	CollectedAt *time.Time `json:"collected_at"`
	// SignedActorID 实际签收人：临时换角对已签收任务无效，故签收后永久锁定原演员。
	SignedActorID *uint  `json:"signed_actor_id"`
	ScanCode      string `json:"scan_code"` // 签收时扫到的条码
	Note          string `json:"note"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	Costume Costume `json:"costume" gorm:"foreignKey:CostumeID"`
	Actor   Actor   `json:"actor" gorm:"foreignKey:ActorID"`
}

// CareRecord 洗护/维修记录。衣服必须完成洗护才能恢复 available。
type CareRecord struct {
	ID            uint       `json:"id" gorm:"primaryKey"`
	CostumeID     uint       `json:"costume_id" gorm:"index;not null"`
	Type          string     `json:"type"` // wash / repair
	Status        string     `json:"status" gorm:"not null;default:'ongoing'"` // ongoing / done
	Reason        string     `json:"reason"`
	StartedAt     time.Time  `json:"start_at" gorm:"not null"`
	CompletedAt   *time.Time `json:"completed_at"`
	Operator      string     `json:"operator"`
	CreatedAt     time.Time  `json:"created_at"`
}

// Incident 少件/破损处理单。只能通过开单+处理关闭，
// 不允许靠“补库存数量”抹平。
type Incident struct {
	ID            uint       `json:"id" gorm:"primaryKey"`
	PerformanceID *uint      `json:"performance_id" gorm:"index"`
	CostumeID     uint       `json:"costume_id" gorm:"index;not null"`
	AssignmentID  *uint      `json:"assignment_id"`
	Type          string     `json:"type" gorm:"not null"`   // lost / damaged
	Status        string     `json:"status" gorm:"index;not null;default:'open'"`
	Description   string     `json:"description"`
	ReportedAt    time.Time  `json:"reported_at" gorm:"not null"`
	ResolvedAt    *time.Time `json:"resolved_at"`
	Resolution    string     `json:"resolution"` // found / repaired / written_off
	Handler       string     `json:"handler"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`

	Costume Costume `json:"costume" gorm:"foreignKey:CostumeID"`
}

// InventoryCheck 散场清点记录（一件衣服一条）。
type InventoryCheck struct {
	ID            uint       `json:"id" gorm:"primaryKey"`
	PerformanceID uint       `json:"performance_id" gorm:"index;not null"`
	CostumeID     uint       `json:"costume_id" gorm:"index;not null"`
	Found         bool       `json:"found"`
	Condition     string     `json:"condition"` // ok / damaged
	CheckedAt     time.Time  `json:"checked_at" gorm:"not null"`
	IncidentID    *uint      `json:"incident_id"`
	Note          string     `json:"note"`
	CreatedAt     time.Time  `json:"created_at"`

	Costume Costume `json:"costume" gorm:"foreignKey:CostumeID"`
}

// RecastLog 临时换角审计记录。
type RecastLog struct {
	ID           uint      `json:"id" gorm:"primaryKey"`
	AssignmentID uint      `json:"assignment_id" gorm:"not null"`
	PerformanceID uint    `json:"performance_id" gorm:"not null"`
	CostumeID    uint      `json:"costume_id" gorm:"not null"`
	FromActorID  uint      `json:"from_actor_id" gorm:"not null"`
	ToActorID    uint      `json:"to_actor_id" gorm:"not null"`
	Reason       string    `json:"reason"`
	ChangedAt    time.Time `json:"changed_at" gorm:"not null"`
	CreatedAt    time.Time `json:"created_at"`
}
