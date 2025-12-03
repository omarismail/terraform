#!/bin/bash

DATA=$(</dev/stdin)
echo "stdin: $DATA, args: $@"

echo "written by args: $@" > action.txt
