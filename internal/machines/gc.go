package machines

/*
N: save all snapshots

Rule 1:
	a snapshot must not be a dependency of a dataset to keep.
	if a dataset must be kept then all its dependencies must be kept.

Current Day:
    Mark as saved all snapshots and compute deps

Next Day (starts @12:00AM)
	Mark as candidate for destruction all the snapshots from previous day but 3 evenly separated and including those kept because they are dependencies of dataset of the current day.



T*..............*................*..............*.............*
R.....#.....................#..................................
K.....O.........O...........O...................O.............O

T*..............*................*..............*.............*
R.....#............#........#.......#.........#.........#......
K.....O............O........O.......O.........O.........O......

T*..............*................*..............*.............*
R.....#............#........#.......#..........................
K.....O............O........O.......O.........................*

T*..............*................*..............*.............*
R.....#...#...#................................................
K.....O...O...O..................?............................?

T*..............*................*..............*.............*
R.......................#...#...#..............................

..............................................................
Next Week (start @day #0)



L *
M
M
J
V
S
D

L *
M *
M *
J *
V *
S *
D *

L ***
M ***
M ******  <- New Month
J
V
S
D

              02/01
   ------------@-----------------------11/04
       01/01   02/01  03/01   04/01
        -------@-------@------@-- 10/04
		       ---------@---------- 12/04
---@----@-------------------@------------@------@---- 20/05
                          03/01        15/05  16/05
*/