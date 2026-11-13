package engine

// Version names the engine model that produced a game: the simulation
// contract a game's commitment binds, its rules, rate tables, arithmetic,
// draw addressing, and the meaning of the events it records. Bump it by hand
// whenever any of those change, so a stored game names its engine. This
// module plays only the current model.
const Version = "4"
