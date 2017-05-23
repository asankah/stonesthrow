import os
import argparse
import sys
import json
import subprocess

class Options(object):

    def __init__(self):
        self.source_path = ''
        self.build_path = ''
        self.platform_name = ''
        self.repository_name = ''

# These are the methods that each module should implement:
#
# def ConfigureFlags(config):
#     return argparse.ArgumentParser() defining all the supported subcommands
#     for the module.
#
# def ListCommands(options):
#     Lists supported commands.
#
# def NeedsSource(options):
#     Returns True iff the command selected in 'options' requires up-to-date
#     sources.
#
# def Run(options):
#     raise RuntimeError('Not implemented')

def _WriteControlData(data):
    sys.stdout.write("\n")
    sys.stdout.write(data)
    sys.stdout.write("\n")
    sys.stdout.flush()

def NotifyStartProcess(args, directory):
    o = {"begin_command_event": { "command": {"command": args, "directory": directory }}}
    _WriteControlData("@@@J:{json}@@@".format(json=json.dumps(o)))

def NotifyEndProcess(return_code=0):
    o = {"end_command_event": { "return_code": return_code }}
    _WriteControlData("@@@J:{json}@@@".format(json=json.dumps(o)))

def Debug(message):
    o = { "log_event": { "msg": message , "severity": 2}}
    _WriteControlData("@@@J:{json}@@@".format(json=json.dumps(o)))

def Info(message):
    o = { "log_event": { "msg": message , "severity": 1}}
    _WriteControlData("@@@J:{json}@@@".format(json=json.dumps(o)))

def Error(message):
    o = { "log_event": { "msg": message , "severity": 0}}
    _WriteControlData("@@@J:{json}@@@".format(json=json.dumps(o)))

def CheckCall(*args, **kwargs):
    return_code = 0
    rv = None
    try:
        NotifyStartProcess(args[0], os.getcwd())
        rv = subprocess.check_call(*args, **kwargs)
    except subprocess.CalledProcessError as e:
        return_code = e.returncode
    except Exception as e:
        return_code = -1
        raise e
    finally:
        NotifyEndProcess(return_code)
    return rv

def CheckOutput(*args, **kwargs):
    return_code = 0
    rv = ''
    if 'universal_newlines' not in kwargs.keys():
        kwargs['universal_newlines'] = True
    directory = os.getcwd() if 'cwd' not in kwargs else kwargs['cwd']

    try:
        NotifyStartProcess(args[0], directory)
        rv = subprocess.check_output(*args, **kwargs)
    except Exception as e:
        return_code = 1
        raise e
    finally:
        NotifyEndProcess(return_code)
    return rv

