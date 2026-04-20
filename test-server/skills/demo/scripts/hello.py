import json
import sys

def main() -> None:
    payload = {
        "argv": sys.argv,
        "argc": len(sys.argv),
        "note": "argv[0] is this script path; argv[1:] are arguments from execute_shell_script args JSON array",
    }
    print(json.dumps(payload, ensure_ascii=True))


if __name__ == "__main__":
    main()
