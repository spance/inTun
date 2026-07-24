package udprelay

func (r *AssociationRegistry) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.byID)
}
