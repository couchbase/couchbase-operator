set datafile separator ','

set xdata time
set timefmt "%Y-%m-%d %H:%M:%S"
set format x "%Y-%m-%d %H:%M:%S"
set xtics rotate

set key autotitle columnhead
set ylabel "CPU"
set xlabel "Time"

set y2tics
set ytics nomirror
set y2label "Memory (Gi)"

set term png size 1280, 720

plot "data.csv" using 1:2 with lines, '' using 1:3 with lines axis x1y2
