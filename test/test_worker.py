import requests
import time
import json
import os

# Configuration
WORKER_URL = "http://127.0.0.1:8889"
HOST = "192.168.26.130"
USERNAME = "test"
PASSWORD = "123456"
SUDO_PASSWORD = "123456"

DB_HOST = "192.168.26.130"
DB_USER = "root"
DB_PASS = "123456"
DB_NAME = "newbee"
DB_PORT = 3306
DB_TYPE = "mysql"

def print_separator(title):
    print("\n" + "="*50)
    print(f" {title}")
    print("="*50)

def wait_for_task(task_id):
    print(f"Waiting for task {task_id}...")
    start_time = time.time()
    while time.time() - start_time < 60:
        try:
        resp = requests.get(f"{WORKER_URL}/api/task/result/{task_id}")
            if resp.status_code == 200:
                data = resp.json()
                if data.get("has_result"):
                    return data
            elif resp.status_code == 404:
                # Task might be running and no result yet (or not found)
                pass
        except Exception as e:
            print(f"Error checking status: {e}")
        
        time.sleep(2)
    return None

def test_command_execution():
    print_separator("Testing Command Execution")
    payload = {
        "target": HOST,
        "username": USERNAME,
        "password": PASSWORD,
        "command": "uname -a",
        "use_sudo": False
    }
    
    try:
        resp = requests.post(f"{WORKER_URL}/api/task/command", json=payload)
        print(f"Submit Status: {resp.status_code}")
        if resp.status_code != 200:
            print(f"Error: {resp.text}")
            return

        task_id = resp.json().get("task_id")
        print(f"Task ID: {task_id}")
        
        result = wait_for_task(task_id)
        if result:
            print(f"Status: {result.get('status')}")
            detailed = result.get('detailed_result', {})
            parsed = result.get('parsed_result', {})
            print(f"Output: {parsed.get('stdout') or detailed.get('result_data')}")
            print(f"Error: {parsed.get('stderr')}")
        else:
            print("Timeout waiting for result")

    except Exception as e:
        print(f"Exception: {e}")

def test_script_execution():
    print_separator("Testing Script Execution")
    script_content = """
#!/bin/bash
echo "Hello from script running on $(hostname)"
echo "Current user: $(whoami)"
"""
    payload = {
        "target": HOST,
        "username": USERNAME,
        "password": PASSWORD,
        "script_content": script_content,
        "script_type": "bash",
        "use_sudo": False
    }
    
    try:
        resp = requests.post(f"{WORKER_URL}/api/task/script", json=payload)
        print(f"Submit Status: {resp.status_code}")
        if resp.status_code != 200:
            print(f"Error: {resp.text}")
            return

        task_id = resp.json().get("task_id")
        print(f"Task ID: {task_id}")
        
        result = wait_for_task(task_id)
        if result:
            print(f"Status: {result.get('status')}")
            detailed = result.get('detailed_result', {})
            parsed = result.get('parsed_result', {})
            print(f"Output: {parsed.get('stdout') or detailed.get('result_data')}")
        else:
            print("Timeout waiting for result")

    except Exception as e:
        print(f"Exception: {e}")

def test_file_transfer():
    print_separator("Testing File Transfer")
    
    # 1. Create a local dummy file (on the worker machine? No, source path is local to the WORKER)
    # Wait, the File Transfer implementation in TaskExecutor uses os.Open(SourcePath).
    # This means the file must exist ON THE WORKER CONTAINER/HOST.
    # Since I cannot easily create a file inside the worker container from here via HTTP API 
    # (unless I use a script task to create it first!), I should do that.
    
    remote_tmp_file = "/tmp/test_upload_source.txt"
    target_dest_file = "/tmp/test_upload_dest.txt"
    
    print(f"Creating source file on worker: {remote_tmp_file}")
    # We can't easily create a file ON THE WORKER via API unless we use a command task 
    # BUT the worker executes commands on the TARGET.
    # The File Transfer task copies from Worker Local -> Target Remote (Upload).
    # So I need a file on the WORKER.
    # Does the worker have any file I can use? Maybe /etc/hosts?
    source_file = "/etc/hosts"
    
    print(f"Using source file on worker: {source_file}")
    
    # Upload Task
    payload = {
        "target": HOST,
        "username": USERNAME,
        "password": PASSWORD,
        "source_path": source_file,
        "target_path": target_dest_file,
        "direction": "upload"
    }
    
    try:
        resp = requests.post(f"{WORKER_URL}/api/task/file", json=payload)
        if resp.status_code == 404:
            print("Error: /api/task/file endpoint not found. Please update the worker.")
            return
        
        print(f"Submit Status: {resp.status_code}")
        if resp.status_code != 200:
            print(f"Error: {resp.text}")
            return

        task_id = resp.json().get("task_id")
        print(f"Upload Task ID: {task_id}")
        
        result = wait_for_task(task_id)
        if result:
            print(f"Upload Status: {result.get('status')}")
            print(f"Result: {result.get('detailed_result', {}).get('result_data')}")
        
        # Verify upload by running command on target
        print("Verifying upload on target...")
        verify_payload = {
            "target": HOST,
            "username": USERNAME,
            "password": PASSWORD,
            "command": f"ls -l {target_dest_file}",
        }
        resp = requests.post(f"{WORKER_URL}/api/task/command", json=verify_payload)
        if resp.status_code == 200:
            tid = resp.json().get("task_id")
            res = wait_for_task(tid)
            if res:
                parsed = res.get('parsed_result', {})
                print(f"Verification: {parsed.get('stdout')}")

        # Download Task (Download the file we just uploaded)
        print("\nTesting File Download...")
        download_dest = "/tmp/test_download_dest.txt"
        download_payload = {
            "target": HOST,
            "username": USERNAME,
            "password": PASSWORD,
            "source_path": target_dest_file, # The file on remote
            "target_path": download_dest,    # The file on worker
            "direction": "download"
        }
        
        resp = requests.post(f"{WORKER_URL}/api/task/file", json=download_payload)
        if resp.status_code == 200:
            tid = resp.json().get("task_id")
            print(f"Download Task ID: {tid}")
            res = wait_for_task(tid)
            if res:
                print(f"Download Status: {res.get('status')}")
                print(f"Result: {res.get('detailed_result', {}).get('result_data')}")
        else:
            print(f"Download Submit Failed: {resp.status_code}")

    except Exception as e:
        print(f"Exception: {e}")

def test_database():
    print_separator("Testing Database")
    
    # 1. Test Connection
    print("1. Testing Connection...")
    conn_payload = {
        "type": DB_TYPE,
        "host": DB_HOST,
        "port": DB_PORT,
        "username": DB_USER,
        "password": DB_PASS,
        "database": DB_NAME
    }
    
    try:
        resp = requests.post(f"{WORKER_URL}/api/db/test", json=conn_payload)
        print(f"Test Connection Status: {resp.status_code}")
        print(resp.json())
        
        if resp.status_code != 200:
            return

        # 2. Create Connection
        print("\n2. Creating Connection...")
        resp = requests.post(f"{WORKER_URL}/api/db/connect", json=conn_payload)
        data = resp.json()
        if not data.get("success"):
            print(f"Failed to connect: {data}")
            return
            
        conn_id = data["data"]["connection_id"]
        print(f"Connection ID: {conn_id}")
        
        # 3. Get Tables
        print("\n3. Getting Tables...")
        resp = requests.get(f"{WORKER_URL}/api/db/tables", params={"connection_id": conn_id, "database": DB_NAME})
        print(f"Tables: {resp.json()}")

        # 4. Execute SQL
        print("\n4. Executing SQL...")
        sql_payload = {
            "connection_id": conn_id,
            "sql": "SELECT 1 as test_val",
            "timeout": 5
        }
        resp = requests.post(f"{WORKER_URL}/api/db/execute", json=sql_payload)
        print(f"SQL Result: {resp.json()}")
        
        # 5. Close Connection
        print("\n5. Closing Connection...")
        requests.delete(f"{WORKER_URL}/api/db/disconnect", params={"connection_id": conn_id})
        print("Connection Closed")

    except Exception as e:
        print(f"Exception: {e}")

if __name__ == "__main__":
print(f"Starting Proxy Tests against {WORKER_URL}")
    test_command_execution()
    test_script_execution()
    test_file_transfer()
    test_database()
