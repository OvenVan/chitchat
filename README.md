# chitchat
-- import "github.com/ovenvan/chitchat"

What's this: This is a **lightweight**, 
support for **high-concurrency** communication Library framework, 
based on golang socket.

What's demo.go/demo_test.go: This is an simple Demo that tells you how to use this framework.

***
## Usage

Whether for server or client, it only takes three steps to get it worked properly: 
1. Tell it the address of the registration (listening); 
2. Tell it how to process the data after reading it; 
3. Correct handling Error notifications.

#### For Server:

```go
type Server interface {
	Listen() error
	Cut() error
	CloseRemote(string) error
	RangeRemoteAddr() []string
	GetLocalAddr() string
	SetDeadLine(time.Duration, time.Duration)
	ErrChan() <-chan Errsocket
	Write(interface{}) error
}
```

**func** `NewServer`
```go
func NewServer(
	ipaddrsocket string,
	delim byte, readfunc ServerReadFunc, additional interface{}) Server
```
The method of registering the server with the framework. 
All you need to do is to tell the framework the 
1. IP address(Addr:Port) being monitored, 
2. the delimiter of one sentence, 
3. the Read method, 
4. and the additional information (could be nil).

if the delimiter is 0, then the framework will NOT send sentence to readfunc until EOF.

**func** `Listen`
```go
func Listen() error
```
The method does not block the process, 
it asynchronously initiates the relevant goroutine to ensure that the framework works properly. 
At this point, Server is ready to start normally.

**func** `Cut`
```go
func Cut() error
```
Terminates the listening service.

**func** `CloseRemote`
```go
func CloseRemote(remoteAddr string) error
```
Closes a connection for a specific IP. If the connection to the IP does not exist, an error will be returned.

**func** `RangeRemoteAddr`
```go
func RangeRemoteAddr() []string
```
Returns all the connections.

**func** `ErrChan`
```go
func ErrChan() <-chan Errsocket
```
Returns the channel allows consumers to receive errors.

...(some other funcs defined in `server interface`)


#### For Client:
```go
type Client interface {
	Dial() error
	Close()
	SetDeadLine(time.Duration)
	ErrChan() <-chan Errsocket
	Write(interface{}) error
	GetRemoteAddr() string
	GetLocalAddr() string
}
```

**func** `NewClient`
```go
func NewClient(
	ipremotesocket string,
	delim byte, readfunc ClientReadFunc, additional interface{}) Client {
```
Same as `NewServer`. however, the readfunc can be set nil.

**func** `Dial() error`
```go
func Dial() error
```
Allows clients to connect to the remote server. You cannot manually select your own IP address for the time being,
 but you can obtain it through the GetLocalAddr () method. This method does not block the process.

**func** `SetDeadLine(time.Duration)`
```go
func SetDeadLine(dDDL time.Duration)
```
This function works before Dial(). It specifies the maximum time for the connection.

## How to write READ FUNC?

When the framework reads the separator, he hands over what he has previously read to the user-defined read func.

For Server and Client, their ReadFunc is the same, however with different names.
```go
type ServerReadFunc func([]byte, ReadFuncer) error
type ClientReadFunc func([]byte, ReadFuncer) error
```
If you need to use additional parameters, you can get it through t.Addon ().

This parameter was already given (additional) when the Server (Client) was initialized.
If the time is set to a null value, additional parameters cannot be obtained by the Addon method at this time.

## Limitations of Write

there are some rules when using Write method:
1. If you try to write to a `struct`, make sure that the `struct` does NOT contain pointer values (including string).
If you want to restore the byte stream to a structure, using `*(**Struct)(unsafe.Pointer(&str))`.
2. If you have a better write method, you can use `func SetWriteFunc(wf)` to set a new method instead of the default ones.

## Demo
A simple heartbeat detection package.

## For More info
[如何使用golang - chitchat](https://www.jianshu.com/p/956c04a9310b)
