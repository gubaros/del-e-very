package models

type ForkEvent struct {
    Forkee  Repository `json:"forkee"`  // Información sobre el repositorio bifurcado
    Sender  User       `json:"sender"`  // Usuario que realizó la bifurcación
}

type Repository struct {
    ID       int64  `json:"id"`
    Name     string `json:"name"`
    FullName string `json:"full_name"`  // Nombre completo, incluyendo el propietario
}

type User struct {
    ID    int64  `json:"id"`
    Login string `json:"login"`
}

type PushEvent struct {
    Ref        string    `json:"ref"`        // Rama a la que se hicieron los commits
    Before     string    `json:"before"`     // SHA del último commit antes del push
    After      string    `json:"after"`      // SHA del último commit después del push
    Commits    []Commit  `json:"commits"`    // Lista de commits
    Repository Repository `json:"repository"` // Repositorio al que se hace push
    Pusher     User       `json:"pusher"`     // Usuario que realiza el push
}

type Commit struct {
    ID        string `json:"id"`
    Message   string `json:"message"`
    Timestamp string `json:"timestamp"`
    Author    User   `json:"author"`
}

