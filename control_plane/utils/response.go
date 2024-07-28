package utils

type Response struct {
	Code int         `json:"code"`
	Data interface{} `json:"data"`
	Msg  string      `json:"msg"`
}

func SuccessResp(data interface{}) Response {
	return Response{
		Code: 0,
		Data: data,
		Msg:  "",
	}
}

func ErrResp(err error) Response {
	return Response{
		Code: 1,
		Data: nil,
		Msg:  err.Error(),
	}
}
