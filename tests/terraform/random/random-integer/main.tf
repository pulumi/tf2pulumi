provider "random" {
}

resource "random_integer" "randint" {
    min = 0
    max = 200
}
