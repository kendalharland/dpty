#!/bin/sh

SESSION_ADDRESS="$1"

# Save terminal state, go raw, connect, restore on exit
old_stty=$(stty -g)
trap "stty '$old_stty'" EXIT
stty raw -echo -icanon
websocat -b "${SESSION_ADDRESS}"
