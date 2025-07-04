#!/usr/bin/env python3
# Input data format csv, each line is allocation event with:
# startTime, endTime, executionTime, jobName, stageName
# (sec)      (sec)    (sec)          (str)    (str)

import csv
import sys
from datetime import datetime
import statistics

POOL_MAX_HOSTS                  = 500
POOL_SCRUBBING_SEC              = 1.5 * 3600  # 1h30m
POOL_MIN_USAGE_SEC              = 24 * 3600  # 24h
POOL_INSTANCE_INITIALIZE_SEC    = 7 * 60  # 7m
POOL_SCRUBBING_DELAY            = 5 * 60  # 5m


class Host:
    # Contains the allocated dedicated hosts
    hosts = dict()
    hosts_count = 0

    @staticmethod
    def allocateOrGet():
        # Check if here is available allocated host
        for i, h in Host.hosts.items():
            if h.state == 'AVAILABLE':
                return Host.hosts[i]

        # Ok, here is no Host available - let's try to allocate one
        if len(Host.hosts) >= POOL_MAX_HOSTS:
            return None

        host = Host()
        Host.hosts[host.uid] = host

        return Host.hosts[host.uid]

    def __init__(self):
        uid = f"h-{Host.hosts_count}"
        Host.hosts_count += 1

        self.uid = uid
        self.allocated_at = Event.current_time
        self.available_at = Event.current_time
        self.state = 'AVAILABLE'
        self.instance = None

        # Release host when it's time come (if it's not busy by any instance)
        Event.add(Event.current_time + POOL_MIN_USAGE_SEC + 1, self.release)

    def release(self):
        # Instance could release the host, so skipping if already released
        if self.state == 'RELEASED':
            return True
        # Delete current host from the list if it's not busy with instance and it's time come
        if (Event.current_time >= (self.allocated_at + POOL_MIN_USAGE_SEC)) and self.instance is None:
            # Add last host monthly statistics
            Event.stat_hosts_hours_per_month[datetime.fromtimestamp(Event.current_time).month-1] += (Event.current_time-self.available_at) / 3600

            self.state = 'RELEASED'
            del Host.hosts[self.uid]
            return True

        # Otherwise do nothing - instance during termination trigger the release as well
        return False

    def scrubbing(self):
        # Run release of the host or scrubbing process instead
        if self.release():
            return False
        # Could be requested again - so we should ignore
        if self.state == 'SCRUBBING':
            return False
        # Auto-scrubbing after delay could try to run when instance already took the host
        if self.instance != None and self.state == 'BUSY':
            return False
        if self.instance != None or self.state not in ('AVAILABLE', 'PRESCRUBBING'):
            raise Exception(f"Unable to set Host.scrubbing when it's in state {self.state} and instance {self.instance}")

        # Add monthly statistics
        Event.stat_hosts_hours_per_month[datetime.fromtimestamp(Event.current_time).month-1] += (Event.current_time-self.available_at) / 3600

        self.state = 'SCRUBBING'
        Event.add(Event.current_time + POOL_SCRUBBING_SEC, self.available)
        return True

    def available(self):
        # Available event could be called after release, so just skipping in this case
        if self.state == 'RELEASED':
            return False
        if self.state != 'SCRUBBING':
            raise Exception(f"Unable to set Host.available when it's in state {self.state}")

        self.state = 'AVAILABLE'
        self.available_at = Event.current_time

        Event.add(Event.current_time + POOL_SCRUBBING_DELAY, self.scrubbing)
        return True

    def busy(self, inst):
        if self.state != 'AVAILABLE':
            raise Exception(f"Unable to set Host.busy when it's in state {self.state}")

        self.state = 'BUSY'
        self.instance = inst

    def __repr__(self):
        return f'Host({self.uid}, {self.allocated_at}, {self.state}, ' + (self.instance.uid if self.instance != None else 'None') + ')'

class Instance:
    # Contains the allocated instances
    instances = dict()
    instances_count = 0

    @staticmethod
    def allocate(start, duration, job, stage):
        # We have to check that this item in the queue, otherwise we could face double-allocations
        if (start, duration, job, stage) not in Event.workload_queue:
            return None

        host = Host.allocateOrGet()
        if host is None:
            return None

        inst = Instance(host, duration, job, stage)
        Instance.instances[inst.uid] = inst

        return Instance.instances[inst.uid]

    def __init__(self, host, duration, job, stage):
        uid = f"i-{Instance.instances_count}"
        Instance.instances_count += 1

        self.uid = uid
        self.allocated_at = Event.current_time
        self.state = 'INITIALIZE'
        self.job = job
        self.stage = stage
        self.host = host

        self.host.busy(self)

        # The instance will be initialized in some time, so creating event for that
        Event.add(Event.current_time + POOL_INSTANCE_INITIALIZE_SEC, self.busy, (duration,))

    def busy(self, duration):
        if self.state != 'INITIALIZE':
            raise Exception(f"Unable to set Instance.busy when it's in state {self.state}")

        self.state = 'BUSY'

        # After duration the instance should be terminated
        Event.add(Event.current_time + duration, self.terminate)

        # Add monthly statistics
        Event.stat_instances_hours_per_month[datetime.fromtimestamp(Event.current_time).month-1] += duration / 3600

    def terminate(self):
        if self.state == 'TERMINATED':
            raise Exception(f"Unable to set Instance.terminate when it's in state {self.state}")
        self.state = 'TERMINATED'
        self.host.instance = None
        self.host.state = 'PRESCRUBBING'
        self.host.scrubbing()

        del Instance.instances[self.uid]

    def __repr__(self):
        return f'Instance({self.uid}, {self.state}, {self.job}, {self.stage}, ' + (self.host.uid if self.host != None else 'None') + ')'

class Event:
    # Used to store the generated events
    events = dict()

    # Stores the jobs that was unable to run right away to retry later
    workload_queue = list()

    # The currently processed event time
    current_time = None

    # Statistics
    stat_instances_max = 0
    stat_instances_hours_per_month = [0]*12
    stat_hosts_max = 0
    stat_hosts_hours_per_month = [0]*12
    stat_queue_max = 0
    stat_queue_wait_min_max = 0
    stat_queue_hours_per_month = [0]*12
    stat_queue_mean_wait_mins_per_month = [[],[],[],[],[],[],[],[],[],[],[],[]]

    @staticmethod
    def workload(start, duration, job, stage):
        # Putting the workload into queue and the allocate event into the events
        Event.workload_queue.append((start, duration, job, stage))

    @staticmethod
    def print():
        busy = len([h for k,h in Host.hosts.items() if h.state == 'BUSY'])
        scrub = len([h for k,h in Host.hosts.items() if h.state == 'SCRUBBING'])
        avail = len([h for k,h in Host.hosts.items() if h.state == 'AVAILABLE'])
        print(datetime.fromtimestamp(Event.current_time), "->",
            "INSTANCES:", len(Instance.instances),
            "HOSTS: total:", len(Host.hosts),
            "busy:", busy,
            "scrub:", scrub,
            "avail:", avail,
            "EVENTS:", len(Event.events),
            "QUEUE:", len(Event.workload_queue)
        )
        #print(Event.current_time, 'AWS: dedicated "mac_ultra": Amount of active hosts in pool:', len(Host.hosts))

    @staticmethod
    def processTick(t):
        # Process the workloads queue and creating allocation events from them to be executed
        for wl in Event.workload_queue:
            Event.add(t, Instance.allocate, wl)

        # Re-running processing if it returns true, because that could cause new events to emerge
        while Event._tick(t):
            pass

        # Printing this tick statistics
        Event.print()

        # Aggregating data
        Event.stat_instances_max = max(Event.stat_instances_max, len(Instance.instances))
        Event.stat_hosts_max = max(Event.stat_hosts_max, len(Host.hosts))
        Event.stat_queue_max = max(Event.stat_queue_max, len(Event.workload_queue))

    @staticmethod
    def _tick(t):
        # Let's make sure all the generated events prior to provided time is completed
        processed = False

        sorted_keys = list(sorted(Event.events.keys()))
        for k in sorted_keys:
            if k > t:
                break
            processed = True

            events = Event.events.pop(k)
            for evt in events:
                Event.current_time = evt.start
                # Print object.name In case it's object method otherwise just class.method
                #if getattr(evt.fun, '__self__', None) != None:
                #    print(datetime.fromtimestamp(Event.current_time) , "-->", f"Event starting: {evt.fun.__self__}.{evt.fun.__name__}{evt.params}")
                #else:
                #    print(datetime.fromtimestamp(Event.current_time) , "-->", f"Event starting: {evt.fun.__qualname__}{evt.params}")

                result = evt.fun(*evt.params)

                # Deleting workload from queue since it has an instance now
                if evt.fun.__qualname__ == Instance.allocate.__qualname__ and result != None:
                    # Add monthly statistics
                    if Event.current_time != evt.params[0]:
                        mon = datetime.fromtimestamp(Event.current_time).month - 1
                        Event.stat_queue_hours_per_month[mon] += (Event.current_time - evt.params[0]) / 3600
                        wait_m = (Event.current_time - evt.params[0]) / 60
                        Event.stat_queue_mean_wait_mins_per_month[mon].append(wait_m)
                        if wait_m > Event.stat_queue_wait_min_max:
                            Event.stat_queue_wait_min_max = wait_m
                        if wait_m > 10:
                            print("WARN: too long time waited in queue:", wait_m, evt.params)
                    Event.workload_queue.remove(evt.params)

                # If this event was availability of the host - then check queue for the pending instances
                # to immediately allocate and not to waste time in waiting
                elif evt.fun.__qualname__ == Host.available.__qualname__ and result == True and len(Event.workload_queue) > 0:
                    events.append(Event(t, Instance.allocate, Event.workload_queue.pop(0)))

        return processed

    @staticmethod
    def add(t, fun, params = ()):
        if t not in Event.events:
            Event.events[t] = []
        Event.events[t].append(Event(t, fun, params))

    @staticmethod
    def complete():
        # Completing the simulation by steps in 10 min
        t = Event.current_time
        while len(Event.workload_queue) > 0 or len(Event.events) > 0:
            t += 600
            Event.processTick(t)

        print()
        print("Simulator statistics:")
        print("Max: Instances:", Event.stat_instances_max, "Hosts:", Event.stat_hosts_max, "Queue:", Event.stat_queue_max, "wait (minutes):", Event.stat_queue_wait_min_max)
        print()
        print("                      ", " ".join(["{:>10s}".format(m) for m in ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"]]))
        print("Instances       h/mon:", " ".join(["{:>10.2f}".format(h) for h in Event.stat_instances_hours_per_month]))
        print("Hosts           h/mon:", " ".join(["{:>10.2f}".format(h) for h in Event.stat_hosts_hours_per_month]))
        print("Queue           h/mon:", " ".join(["{:>10.2f}".format(hs) for hs in Event.stat_queue_hours_per_month]))
        # Mean wait means - if the item got into queue - for how long it usually waits before get executed
        print("Queue Mean wait m/mon:", " ".join(["{:>10.2f}".format((statistics.mean(mm)) if mm else 0.0) for mm in Event.stat_queue_mean_wait_mins_per_month]))

    def __init__(self, start, fun, params):
        self.start = start
        self.fun = fun
        self.params = params

    def __repr__(self):
        return f'Event({self.start}, {self.fun}, {self.params})'


# Running the process reading the incoming data
# During it the new events will be generated and if the generated events are earlier - they will be
# executed first. So in general we skipping the no-event times and recalculating the world only on
# event occurance. Input data should be pre-sorted, otherwise the time continuum will be broken.
with open(sys.argv[1], newline='') as csvfile:
    rdr = csv.DictReader(csvfile)
    for row in rdr:
        #print(row)
        Event.workload(int(row['startTime']), int(row['executionTime']), row['jobName'], row['stageName'])

        # Processing the events up to this point in events list
        Event.processTick(int(row['startTime']))

        #print("Instances:", Instance.instances)
        #print("Hosts:", Host.hosts)
        #print("Events:", Event.events)

    # When we're out of workloads - gracefully shutdown to make sure the simulation is working fine
    Event.complete()
