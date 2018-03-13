package main

import (
	"encoding/json"
	"errors"
	"github.com/segmentio/ksuid"
	"sync"
)

//var GameRooms map[string]*GameRoom

func NewGameRoom(number int) (gameRoom *GameRoom) {
	gameRoom = &GameRoom{
		Broadcast:       make(chan []byte),
		Register:        make(chan *Connection),
		Unregister:      make(chan *Connection),
		Connections:     make(map[*Connection]bool, number),
		LuckCards:       make(map[LUCK_CARD_TYPE_ENUM]bool, int(LUCK_CARD_TYPE__MAX)),
		NewsCards:       make(map[NEWS_CARD_TYPE_ENUM]bool, int(NEWS_CARD_TYPE__MAX)),
		MaxClientNumber: number,
		Bank:            INITIAL_BANK_MONEY,
	}
	//初始化roomid
	gameRoom.Id = newID()
	//启动房间注册函数
	go gameRoom.run()
	//初始化运气池
	gameRoom.InitLuckCardMap()
	//初始化新闻池
	gameRoom.InitNewsCardMap()
	//初始化地图
	gameRoom.InitGameMap()

	gameRoom.RoomStatus = GAMEROOM_STATUS__ENABLE
	return
}

func (r *GameRoom) InitLuckCardMap() {
	for i := int(LUCK_CARD_TYPE__MIN) + 1; i < int(LUCK_CARD_TYPE__MAX); i++ {
		r.LuckCards[LUCK_CARD_TYPE_ENUM(i)] = true
	}
}

func (r *GameRoom) InitNewsCardMap() {
	for i := int(NEWS_CARD_TYPE__MIN) + 1; i < int(NEWS_CARD_TYPE__MAX); i++ {
		r.NewsCards[NEWS_CARD_TYPE_ENUM(i)] = true
	}
}

func (r *GameRoom) LuckCard() (cardNo int) {
	cardNo = RandNumber() % int(LUCK_CARD_TYPE__MAX-1)
	if r.LuckCards[LUCK_CARD_TYPE_ENUM(cardNo)] == true {
		r.LuckCards[LUCK_CARD_TYPE_ENUM(cardNo)] = false
		return cardNo
	} else {
		var flag bool = false
		for idx, d := range r.LuckCards {
			if d == true {
				flag = true
				return int(idx)
			}
		}
		if flag == false {
			r.InitLuckCardMap()
			return r.LuckCard()
		}
	}
	return cardNo
}

func (r *GameRoom) NewsCard() (cardNo int) {
	cardNo = RandNumber() % int(NEWS_CARD_TYPE__MAX-1)
	if r.NewsCards[NEWS_CARD_TYPE_ENUM(cardNo)] == true {
		r.NewsCards[NEWS_CARD_TYPE_ENUM(cardNo)] = false
		return cardNo
	} else {
		var flag bool = false
		for idx, d := range r.NewsCards {
			if d == true {
				flag = true
				return int(idx)
			}
		}
		if flag == false {
			r.InitNewsCardMap()
			return r.NewsCard()
		}
	}
	return cardNo
}

//初始化地图
func (r *GameRoom) InitGameMap() {
	r.Map.Map = InitialGameMap.Map
}

//根据游戏房间ID号，获取游戏房间信息
func GetGameRoomById(id string) (gameRoom *GameRoom) {
	return h.GameRooms[id]
}

//根据游戏房间ID号，获取游戏房间用户的基础信息 FIXME ： 需要删除session信息
func GetGameRoomClientInfo(id string) (ClientInfoList []ClientInfo) {
	for c, _ := range h.GameRooms[id].Connections {
		ClientInfoList = append(ClientInfoList, c.ClientInfo)
	}
	return ClientInfoList
}

//房间内的游戏用户移动其摇到的骰子的距离
func (room *GameRoom) GameUserMove(dice int, c *Connection) (err error) {
	var dstDice int = dice
	var ok bool
	var pos Pos
	if pos, ok = room.Map.CurrentUserLocation[c]; ok {
		idx, _, err := c.GetMapLocation(pos, room)
		if err != nil {
			return errors.New("move error")
		}
		mapLen := len(room.Map.CurrentUserLocation)
		//如果已经是再次经过，则跳过起点
		if idx+dice >= mapLen+1 {
			dstDice = dice + 1

			//如果已经再过起点，则再给用户一部分钱
			room.BankSendMony(c, BANK_SEND_MONY)
		}
		//获取移动距离后的点的坐标
		mapPos := room.Map.Map[idx+dstDice]

		//更新坐标位置
		pos.LocationX = mapPos.LocationX
		pos.LocationY = mapPos.LocationY
	}
	//广播移动位置
	var userMove MessageGameUserMove
	userMove.MessageType = MESSAGE_TYPE__GAME_USER_MOVE
	userMove.MoveStep = dice
	userMove.MovePos = pos
	userMove.GameRoomId = room.Id
	data, _ := json.Marshal(userMove)
	room.Broadcast <- data

	//TODO 移动到的位置，判断是否收租金，是否买地，是否不够钱需要抵押房产
	room.GameDoing(c)

	//判断输赢
	room.CheckGameDone()
	return nil
}

//银行给用户派送钱
func (room *GameRoom) BankSendMony(c *Connection, mony int64) (err error) {
	var lock sync.Mutex
	lock.Lock()
	defer lock.Unlock()
	//银行支出
	room.Bank = room.Bank - mony
	//用户增加钱
	room.Money[c] = room.Money[c] + mony

	return nil
}

//用户掷完骰子后，检查需要做的动作，比如：付租金，买地，升级地产，抵押地产来付租金，
func (room *GameRoom) GameDoing(c *Connection) (err error) {
	var confirmData []byte
	for con, data := range room.Map.ClientMap {
		for index, mapData := range data {
			//过路/自己的地，需要支付租金/升级地产
			if room.Map.CurrentUserLocation[c].IsEqual(Pos{mapData.LocationX, mapData.LocationY}) {
				if con == c {
					//TODO 自己的地，确认是否升级地产
					var land MessageUserLandUpdate
					land.Land = room.Map.ClientMap[con][index]
					land.UpdateFee = land.Land.Fee + int64(float64(land.Land.Level)*0.2+float64(land.Land.Level))*land.Land.Fee
					land.GameRoomId = room.Id
					land.MessageType = MESSAGE_TYPE__LAND_UPDATE
					confirmData, _ = json.Marshal(land)

				} else {
					//TODO 路过别人的地，需要支付租金
					room.Money[c] = room.Money[c] - room.Map.ClientMap[con][index].RentFee
					room.Money[con] = room.Money[con] + room.Map.ClientMap[con][index].RentFee
					var land MessageUserPayRenFee
					land.RentFee = room.Map.ClientMap[con][index].RentFee
					land.GameRoomId = room.Id
					land.Land = room.Map.ClientMap[con][index]
					land.MessageType = MESSAGE_TYPE__PAY_RENT_FEE

					confirmData, _ = json.Marshal(land)
				}
			}
			//空地，发送消息是否买地
			var land MessageUserBuyLand
			land.Land = room.Map.ClientMap[con][index]
			land.GameRoomId = room.Id
			land.MessageType = MESSAGE_TYPE__BUY_LAND

			confirmData, _ = json.Marshal(land)
			//
		}
	}

	//发送消息，确认是否操作
	c.Send <- confirmData

	return nil
}

//用户地产抵押
func (room *GameRoom) LandImpawn(c *Connection, mapList []MapElement) (err error) {
	for idx, data := range mapList {
		if room.Map.ClientMap[c][idx].IsEqual(data) {
			//判断地产是否是同一个地产，如果是同一个地产，则把地产赎回，并根据地产计算费用
			//支付费用
			room.Money[c] = room.Money[c] + room.Map.ClientMap[c][idx].Fee
			//把地产变为可用
			room.Map.ClientMap[c][idx].Status = 0
		}
	}

	return nil
}

//用户地产赎回
func (room *GameRoom) LandRedeem(c *Connection, mapList []MapElement) {
	for idx, data := range mapList {
		if room.Map.ClientMap[c][idx].IsEqual(data) {
			//判断地产是否是同一个地产，如果是同一个地产，则把地产赎回，并根据地产计算费用
			//支付费用
			room.Money[c] = room.Money[c] - room.Map.ClientMap[c][idx].Fee
			//把地产变为可用
			room.Map.ClientMap[c][idx].Status = 1
		}
	}

	return
}

//判断游戏是否结束
func (r *GameRoom) CheckGameDone() (done bool, err error) {
	var count int = 0
	for _, data := range r.Money {
		if data >= GAME_DOEN_MONY {
			return true, nil
		}
		//FIXME 还需要判断用户是否还有地产，如果地产，则说明其还可以进行抵押
		if data <= 0 {
			count++
		}
		//已经是最后的一个用户
		if count >= len(r.Connections)-1 {
			return true, nil
		}
	}

	return false, nil
}

//判断一个点是否跟自己属于同一个点
func (this Pos) IsEqual(pos Pos) bool {
	if this.LocationX == pos.LocationX && this.LocationY == pos.LocationY {
		return true
	}

	return false
}

//判断一个地图元素是否跟自己是同一个地图元素
func (m MapElement) IsEqual(m1 MapElement) bool {
	if m.LocationX == m1.LocationX && m.LocationY == m1.LocationY {
		return true
	}
	return false
}

func newID() string {
	return ksuid.New().String()
}

func (r *GameRoom) run() {
	for {
		select {
		case c := <-r.Register:
			//用户连接变为可用
			r.Connections[c] = true
			//初始化钱
			r.Money[c] = INITIAL_MONEY
			//初始化位置到起点
			r.Map.CurrentUserLocation[c] = Pos{LocationX: r.Map.Map[0].LocationX, LocationY: r.Map.Map[0].LocationY}
		case c := <-r.Unregister:
			if _, ok := r.Connections[c]; ok {
				r.Connections[c] = false
				//FIXME 回收地产
				//FIXME 重置为起点
				//FIXME 回收钱
				delete(r.Connections, c)
				close(c.Send)
			}
		case m := <-r.Broadcast:
			for c := range r.Connections {
				select {
				case c.Send <- m:
				default:
					delete(r.Connections, c)
					close(c.Send)
				}
			}
		}
	}
}
