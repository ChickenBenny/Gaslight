package chain

const headSubBuffer = 64

func (d *Driver) SubscribeHeads() (<-chan *Block, func()) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.subs == nil {
		d.subs = make(map[uint64]chan *Block)
	}
	d.subSeq++
	id := d.subSeq
	ch := make(chan *Block, headSubBuffer)
	d.subs[id] = ch

	unsub := func() {
		d.mu.Lock()
		defer d.mu.Unlock()
		if _, ok := d.subs[id]; !ok {
			return
		}
		delete(d.subs, id)
		close(ch)
	}
	return ch, unsub
}

func (d *Driver) notifyHeads(head *Block) {
	for id, ch := range d.subs {
		select {
		case ch <- head:
		default:
			delete(d.subs, id)
			close(ch)
		}
	}
}
