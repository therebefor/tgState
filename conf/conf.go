package conf

var BotToken string
var ChannelName string
var Pass string
var Mode string
var BaseUrl string
var Codes string

type UploadResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	ImgUrl  string `json:"url"`
}

// RedeemResponse 兑换码接口的响应
type RedeemResponse struct {
	Code      int    `json:"code"`
	Message   string `json:"message"`
	Extra     int    `json:"extra,omitempty"`
	Unlimited bool   `json:"unlimited,omitempty"`
}

const FileRoute = "/d/"
