package stonesthrow

type Connection interface {
	Send(message interface{}) error
	Receive() (interface{}, error)
	Close()
}
