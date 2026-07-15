package ctrl

// AEADName documents the fixed AEAD used by the underlying EWP control
// channel. It is not configurable (see DESIGN.md §11.4); this constant
// exists only for logging / diagnostics.
const AEADName = "ChaCha20-Poly1305"
