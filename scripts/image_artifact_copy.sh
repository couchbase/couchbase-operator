#!/bin/bash
# Parses the dockerignore files for explicit includes and copies them to a destination folder
set -eu

usage() { echo "Usage: $0 [-i <string>] [-o <string>]" 1>&2; exit 1; }

while getopts ":i:o:" a; do
    case "${a}" in
        i)
            IGNOREFILE=${OPTARG}
            ;;
        o)
            OUTDIR=${OPTARG}
            ;;
        *)
            usage
            ;;
    esac
done
shift $((OPTIND-1))

if [ -z "${IGNOREFILE}" ] || [ -z "${OUTDIR}" ]; then
    usage
fi

BASEDIR=$(dirname "$0")

while IFS= read -r FILE || [[ -n "$FILE" ]]
do
    PARSED=$(echo $FILE | grep ! | cut -d ! -f 2 | sed 's/\/\*//g' )
    if [ -z "$PARSED" ]
    then
        continue
    fi

    echo "Copying $PARSED to ${OUTDIR}"
    cp --parent -r ${PARSED} "${OUTDIR}"
done < "${IGNOREFILE}"
