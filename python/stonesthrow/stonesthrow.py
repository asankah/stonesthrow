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
#     return argparse.ArgumentParser()
# 
# def GetDependentBuildTargets(options, config):
#     return []
# 
# def Run(options, config):
#     raise RuntimeError('Not implemented')
# 

def NotifyStartProcess(*args):
    o = { "cmdline": args }
    sys.stdout.write("@@@BeginCommand:{json}@@@\n".format(json=json.dumps(o)))

def NotifyEndProcess(succeeded=True):
    o = { "result": succeeded }
    sys.stdout.write("@@@EndCommand:{json}@@@\n".format(json=json.dumps(o)))

def CheckCall(*args, **kwargs):
    succeeded = True
    rv = None
    try:
        NotifyStartProcess(args)
        rv = subprocess.check_call(*args, **kwargs)
    except Exeception as e:
        succeeded = False
        raise e
    finally:
        NotifyEndProcess(succeeded)
    return rv

def CheckOutput(*args, **kwargs):
    succeeded = True
    rv = None
    try:
        NotifyStartProcess(args)
        rv = subprocess.check_output(*args, **kwargs)
    except Exeception as e:
        succeeded = False
        raise e
    finally:
        NotifyEndProcess(succeeded)
    return rv

