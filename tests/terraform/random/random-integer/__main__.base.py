import pulumi
import json
import pulumi_random as random

randint = random.RandomInteger("randint",
    max=200,
    min=0)
