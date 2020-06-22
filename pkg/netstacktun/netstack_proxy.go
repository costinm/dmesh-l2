package netstacktun

//type epProxy struct {
//	waitEntry waiter.Entry
//	notifyCh  chan struct{}
//}

//type epProxyConn struct {
//	wq *waiter.Queue
//	ep tcpip.Endpoint
//}
//
//func newEpProxy() *epProxy {
//	ep := &epProxy{}
//	ep.waitEntry, ep.notifyCh = waiter.NewChannelEntry(nil)
//	return ep
//}

//func (ep *epProxy) Run() {
//	for {
//		select {
//		case e := <-ep.notifyCh:
//			log.Println(e)
//		}
//	}
//}
//
//func (*epProxy) Add(wq *waiter.Queue, ep tcpip.Endpoint) {
//
//}
//
//func (*epProxy) Close() {
//
//}

//type callbackEntry struct {
//	ep tcpip.Endpoint
//}
//
//func (c *callbackEntry) Callback(*waiter.Entry) {
//
//}

//func newProxyEntry(ep tcpip.Endpoint) *waiter.Entry {
//	return &waiter.Entry{
//		Context:  ep,
//		Callback: &callbackEntry{ep: ep},
//	}
//}

//// All tcp connections are received here.
//func (*epProxy) proxyHandler(wq *waiter.Queue, ep tcpip.Endpoint) {
//	//conn := gonet.NewConn(wq, ep)
//	//defer conn.Close()
//
//	defer ep.Close()
//
//	// Create wait queue entry that notifies a channel.
//	waitEntry, notifyCh := waiter.NewChannelEntry(nil)
//
//	// EventRegister adds a Callback. Callback will take the entry as param
//	// NewChannelEntry creates an entry where the callback posts on a channel
//	wq.EventRegister(&waitEntry, waiter.EventIn)
//
//	// All readable events reported on the callback, it'll continue reading
//	readCB := newProxyEntry(ep)
//	wq.EventRegister(readCB, waiter.EventIn)
//
//	defer wq.EventUnregister(&waitEntry)
//
//	// This is the 'original destination'
//	la, err := ep.GetLocalAddress()
//	if err != nil {
//		log.Println("LocalAddress", err)
//		return
//	}
//
//	// Doesn't matter, typically a local address on 10.12.0.1
//	ra, err := ep.GetRemoteAddress()
//	if err != nil {
//		log.Println("LocalAddress", err)
//		return
//	}
//
//	log.Println("LA=", la, " ra=", ra)
//
//	for {
//		v, _, err := ep.Read(nil)
//		if err != nil {
//			if err == tcpip.ErrWouldBlock {
//				<-notifyCh
//				continue
//			}
//
//			return
//		}
//
//		ep.Write(tcpip.SlicePayload(v), tcpip.WriteOptions{})
//	}
//}

//func echo(wq *waiter.Queue, ep tcpip.Endpoint) {
//	defer ep.Close()
//
//	// Create wait queue entry that notifies a channel.
//	waitEntry, notifyCh := waiter.NewChannelEntry(nil)
//
//	wq.EventRegister(&waitEntry, waiter.EventIn)
//	defer wq.EventUnregister(&waitEntry)
//
//	for {
//		v, _, err := ep.Read(nil)
//		if err != nil {
//			if err == tcpip.ErrWouldBlock {
//				<-notifyCh
//				continue
//			}
//
//			return
//		}
//
//		ep.Write(tcpip.SlicePayload(v), tcpip.WriteOptions{})
//	}
//}

//// writer reads from standard input and writes to the endpoint until standard
//// input is closed. It signals that it's done by closing the provided channel.
//func writer(ch chan struct{}, ep tcpip.Endpoint) {
//	defer func() {
//		ep.Shutdown(tcpip.ShutdownWrite)
//		close(ch)
//	}()
//
//	r := bufio.NewReader(os.Stdin)
//	for {
//		v := buffer.NewView(1024)
//		n, err := r.Read(v)
//		if err != nil {
//			return
//		}
//
//		v.CapLength(n)
//		for len(v) > 0 {
//			n, err := ep.Write(tcpip.SlicePayload(v), tcpip.WriteOptions{})
//			if err != nil {
//				fmt.Println("Write failed:", err)
//				return
//			}
//
//			v.TrimFront(int(n))
//		}
//	}
//}

// ----------- Not used yet.

//func tcpClient(nt *NetstackTun) error {
//	var wq waiter.Queue
//	ep, err := nt.IPStack.NewEndpoint(tcp.ProtocolNumber, ipv4.ProtocolNumber, &wq)
//	if err != nil {
//		return errors.New(err.String())
//	}
//	defer ep.Close()
//
//	waitEntry, notifyCh := waiter.NewChannelEntry(nil)
//	wq.EventRegister(&waitEntry, waiter.EventOut)
//	terr := ep.Connect(tcpip.FullAddress{Port: 1000, Addr: tcpip.Address("\x0a\x0c\x00\x01")})
//	if terr == tcpip.ErrConnectStarted {
//		fmt.Println("Connect is pending...")
//		<-notifyCh
//		terr = ep.GetSockOpt(tcpip.ErrorOption{})
//	}
//	wq.EventUnregister(&waitEntry)
//	if terr != nil {
//		return errors.New(terr.String())
//	}
//	// Start the writer in its own goroutine.
//	writerCompletedCh := make(chan struct{})
//	go writer(writerCompletedCh, ep)
//
//	// Read data and write to standard output until the peer closes the
//	// connection from its side.
//	wq.EventRegister(&waitEntry, waiter.EventIn)
//	for {
//		v, _, err := ep.Read(nil)
//		if err != nil {
//			if err == tcpip.ErrClosedForReceive {
//				break
//			}
//
//			if err == tcpip.ErrWouldBlock {
//				<-notifyCh
//				continue
//			}
//
//			log.Fatal("Read() failed:", err)
//		}
//
//		os.Stdout.Write(v)
//	}
//	wq.EventUnregister(&waitEntry)
//
//	// The reader has completed. Now wait for the writer as well.
//	<-writerCompletedCh
//
//	ep.Close()
//	return nil
//}
